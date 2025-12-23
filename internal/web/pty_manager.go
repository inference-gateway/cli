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

// Session interface for both local PTY and future SSH sessions
type Session interface {
	Start(cols, rows int) error
	Stop() error
	HandleConnection(conn *websocket.Conn) error
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
	s.cmd.Env = append(os.Environ(), "TERM=xterm-256color")

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

func (s *LocalPTYSession) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	logger.Info("Stopping PTY session...")

	if s.cmd == nil || s.cmd.Process == nil {
		return s.closePTYOnly()
	}

	return s.shutdownProcess()
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
