package web

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	pty "github.com/creack/pty"
	websocket "github.com/gorilla/websocket"
	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// SessionHandler interface for both local PTY and remote SSH sessions
type SessionHandler interface {
	Start(cols, rows int) error
	Resize(cols, rows int) error
	HandleConnection(conn *websocket.Conn) error
	Close() error
}

// Session is an alias for backward compatibility
type Session = SessionHandler

// CreateSessionHandler creates either a local PTY session or remote SSH session
func CreateSessionHandler(webCfg *config.WebConfig, serverCfg *config.SSHServerConfig, cfg *config.Config, v *viper.Viper, sessionID string, sessionManager *SessionManager) (SessionHandler, error) {
	if serverCfg != nil {
		return createRemoteSSHSession(webCfg, serverCfg, cfg.Gateway.URL, sessionID, sessionManager)
	}

	logger.Info("Creating local PTY session")
	return NewLocalPTYSession(cfg, v), nil
}

// createRemoteSSHSession creates a remote SSH session with optional auto-install
func createRemoteSSHSession(webCfg *config.WebConfig, serverCfg *config.SSHServerConfig, gatewayURL string, sessionID string, sessionManager *SessionManager) (SessionHandler, error) {
	logger.Info("Creating remote SSH session", "server", serverCfg.Name)

	client, err := NewSSHClient(&webCfg.SSH, serverCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH client: %w", err)
	}

	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to SSH server: %w", err)
	}

	if err := ensureRemoteBinary(client, webCfg, serverCfg, gatewayURL); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			logger.Warn("Failed to close SSH client after install error", "error", closeErr)
		}
		return nil, err
	}

	if err := ensureRemoteConfig(client, serverCfg, gatewayURL); err != nil {
		logger.Warn("Failed to ensure remote config, continuing anyway", "error", err)
	}

	session, err := NewSSHSession(client, serverCfg, gatewayURL, sessionID, sessionManager)
	if err != nil {
		if closeErr := client.Close(); closeErr != nil {
			logger.Warn("Failed to close SSH client after session error", "error", closeErr)
		}
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}

	return session, nil
}

// ensureRemoteBinary installs infer binary on remote server if auto-install is enabled
func ensureRemoteBinary(client *SSHClient, webCfg *config.WebConfig, serverCfg *config.SSHServerConfig, gatewayURL string) error {
	autoInstall := webCfg.SSH.AutoInstall
	if serverCfg.AutoInstall != nil {
		autoInstall = *serverCfg.AutoInstall
	}

	if !autoInstall {
		return nil
	}

	installer := NewRemoteInstaller(client, &webCfg.SSH, serverCfg, gatewayURL)
	if err := installer.EnsureBinary(); err != nil {
		return fmt.Errorf("failed to ensure infer binary: %w", err)
	}

	return nil
}

// ensureRemoteConfig ensures infer config exists on remote server
// Runs infer init --userspace if ~/.infer/config.yaml doesn't exist
func ensureRemoteConfig(client *SSHClient, serverCfg *config.SSHServerConfig, _ string) error {
	commandPath := serverCfg.CommandPath
	if commandPath == "" {
		commandPath = "infer"
	}

	logger.Info("Checking if infer config exists on remote server", "server", serverCfg.Name)

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer func() { _ = session.Close() }()

	checkCmd := "test -f ~/.infer/config.yaml && echo 'exists' || echo 'missing'"
	output, err := session.CombinedOutput(checkCmd)
	if err != nil {
		return fmt.Errorf("failed to check config file: %w", err)
	}

	outputStr := string(output)
	if len(outputStr) > 0 && outputStr[0] == 'e' {
		logger.Info("Infer config already exists on remote server", "server", serverCfg.Name)
		return nil
	}

	logger.Info("Infer config not found, running init...", "server", serverCfg.Name)

	session2, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session for init: %w", err)
	}
	defer func() { _ = session2.Close() }()

	initCmd := fmt.Sprintf("%s init --userspace", commandPath)
	initOutput, err := session2.CombinedOutput(initCmd)
	if err != nil {
		return fmt.Errorf("failed to initialize config: %w\nOutput: %s", err, string(initOutput))
	}

	logger.Info("Infer config initialized", "server", serverCfg.Name, "output", string(initOutput))
	return nil
}

// LocalPTYSession represents a single local terminal session
type LocalPTYSession struct {
	cfg   *config.Config
	viper *viper.Viper
	pty   *os.File
	cmd   *exec.Cmd
	mu    sync.Mutex
}

func NewLocalPTYSession(cfg *config.Config, v *viper.Viper) *LocalPTYSession {
	return &LocalPTYSession{
		cfg:   cfg,
		viper: v,
	}
}

func (s *LocalPTYSession) Start(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	s.cmd = exec.Command(execPath, "chat")
	s.cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"INFER_WEB_MODE=true",
	)

	ptyFile, err := pty.Start(s.cmd)
	if err != nil {
		return fmt.Errorf("failed to start PTY: %w", err)
	}

	s.pty = ptyFile

	ws := &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		s.pty.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(ws)),
	)
	if errno != 0 {
		logger.Warn("Failed to set initial window size", "error", errno)
	}

	logger.Info("PTY session started", "pid", s.cmd.Process.Pid, "size", fmt.Sprintf("%dx%d", cols, rows))
	return nil
}

// Resize changes the PTY window size
func (s *LocalPTYSession) Resize(cols, rows int) error {
	return s.setWindowSize(cols, rows)
}

// Close terminates the PTY session
func (s *LocalPTYSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	logger.Info("Stopping PTY session...")

	if s.cmd == nil || s.cmd.Process == nil {
		return s.closePTYOnly()
	}

	return s.shutdownProcess()
}

// Stop is deprecated, use Close instead
func (s *LocalPTYSession) Stop() error {
	return s.Close()
}

func (s *LocalPTYSession) closePTYOnly() error {
	if s.pty != nil {
		if err := s.pty.Close(); err != nil {
			logger.Warn("Failed to close PTY", "error", err)
		}
		s.pty = nil
	}
	return nil
}

func (s *LocalPTYSession) shutdownProcess() error {
	pid := s.cmd.Process.Pid
	logger.Info("Initiating graceful shutdown of PTY process", "pid", pid)

	if s.pty != nil {
		logger.Info("Closing PTY file descriptor", "pid", pid)
		if err := s.pty.Close(); err != nil {
			logger.Warn("Failed to close PTY", "pid", pid, "error", err)
		}
		s.pty = nil
	}

	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()

	if s.waitForExit(done, pid, 2*time.Second, "PTY close") {
		return nil
	}

	logger.Info("Process didn't exit after PTY close, sending SIGTERM", "pid", pid)
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		logger.Warn("Failed to send SIGTERM", "pid", pid, "error", err)
	}

	if s.waitForExit(done, pid, 10*time.Second, "SIGTERM") {
		return nil
	}

	logger.Warn("Process didn't exit after SIGTERM, sending SIGKILL", "pid", pid)
	if err := s.cmd.Process.Kill(); err != nil {
		logger.Warn("Failed to kill PTY process", "pid", pid, "error", err)
	}
	<-done
	logger.Info("PTY process forcefully killed", "pid", pid)

	return nil
}

func (s *LocalPTYSession) waitForExit(done chan error, pid int, timeout time.Duration, stage string) bool {
	select {
	case err := <-done:
		if err != nil {
			logger.Info("PTY process exited with error after "+stage, "pid", pid, "error", err)
		} else {
			logger.Info("PTY process exited gracefully after "+stage, "pid", pid)
		}
		return true
	case <-time.After(timeout):
		return false
	}
}

func (s *LocalPTYSession) HandleConnection(conn *websocket.Conn) error {
	done := make(chan struct{})

	go func() {
		defer func() { done <- struct{}{} }()
		for {
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage && s.handleControlMessage(data) {
				continue
			}
			if _, err := s.pty.Write(data); err != nil {
				logger.Error("PTY write error", "error", err)
				return
			}
		}
	}()

	go func() {
		defer func() { done <- struct{}{} }()
		buf := make([]byte, 4096)
		for {
			n, err := s.pty.Read(buf)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				logger.Error("WebSocket write error", "error", err)
				return
			}
		}
	}()

	<-done
	return nil
}

func (s *LocalPTYSession) handleControlMessage(data []byte) bool {
	var msg struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return false
	}
	if msg.Type == "resize" {
		if err := s.setWindowSize(msg.Cols, msg.Rows); err != nil {
			logger.Warn("Failed to resize window", "error", err)
		}
		return true
	}
	return false
}

func (s *LocalPTYSession) setWindowSize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pty == nil {
		return fmt.Errorf("PTY not initialized")
	}

	ws := &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		s.pty.Fd(),
		syscall.TIOCSWINSZ,
		uintptr(unsafe.Pointer(ws)),
	)
	if errno != 0 {
		return fmt.Errorf("failed to set window size: %v", errno)
	}

	return nil
}
