package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	websocket "github.com/gorilla/websocket"
	ssh "golang.org/x/crypto/ssh"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// SSHSession wraps an SSH session with PTY for remote terminal access
type SSHSession struct {
	sshClient *SSHClient
	server    *config.SSHServerConfig
	session   *ssh.Session
	stdin     io.WriteCloser
	stdout    io.Reader
	stderr    io.Reader
	mu        sync.Mutex
	running   bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewSSHSession creates a new SSH session with PTY
func NewSSHSession(client *SSHClient, server *config.SSHServerConfig) (*SSHSession, error) {
	if client == nil {
		return nil, fmt.Errorf("SSH client is required")
	}
	if server == nil {
		return nil, fmt.Errorf("server configuration is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &SSHSession{
		sshClient: client,
		server:    server,
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Start executes "infer chat" on the remote server with PTY
func (s *SSHSession) Start(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("session already started")
	}

	session, err := s.sshClient.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	s.session = session

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,     // Enable echoing
		ssh.TTY_OP_ISPEED: 14400, // Input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // Output speed = 14.4kbaud
	}

	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		if closeErr := session.Close(); closeErr != nil {
			logger.Warn("Failed to close session after PTY request failure", "error", closeErr)
		}
		return fmt.Errorf("failed to request PTY: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		if closeErr := session.Close(); closeErr != nil {
			logger.Warn("Failed to close session after stdin pipe failure", "error", closeErr)
		}
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	s.stdin = stdin

	stdout, err := session.StdoutPipe()
	if err != nil {
		if closeErr := session.Close(); closeErr != nil {
			logger.Warn("Failed to close session after stdout pipe failure", "error", closeErr)
		}
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	s.stdout = stdout

	stderr, err := session.StderrPipe()
	if err != nil {
		if closeErr := session.Close(); closeErr != nil {
			logger.Warn("Failed to close session after stderr pipe failure", "error", closeErr)
		}
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	s.stderr = stderr

	commandPath := s.server.CommandPath
	if commandPath == "" {
		commandPath = "infer"
	}

	cmdArgs := append([]string{"chat"}, s.server.CommandArgs...)
	cmd := fmt.Sprintf("%s %s", commandPath, strings.Join(cmdArgs, " "))

	logger.Info("Starting remote command",
		"command", cmd,
		"server", s.server.Name,
		"cols", cols,
		"rows", rows)

	if err := session.Start(cmd); err != nil {
		if closeErr := session.Close(); closeErr != nil {
			logger.Warn("Failed to close session after command start failure", "error", closeErr)
		}
		return fmt.Errorf("failed to start remote command: %w", err)
	}

	s.running = true

	go func() {
		err := session.Wait()
		if err != nil {
			logger.Error("SSH session exited with error", "error", err, "server", s.server.Name)
		} else {
			logger.Info("SSH session exited normally", "server", s.server.Name)
		}
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		s.cancel()
	}()

	return nil
}

// Resize changes the PTY window size
func (s *SSHSession) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return fmt.Errorf("session not started")
	}

	logger.Info("Resizing terminal", "cols", cols, "rows", rows, "server", s.server.Name)

	if err := s.session.WindowChange(rows, cols); err != nil {
		return fmt.Errorf("failed to resize window: %w", err)
	}

	return nil
}

// HandleConnection bridges WebSocket and SSH session I/O
func (s *SSHSession) HandleConnection(conn *websocket.Conn) error {
	var wg sync.WaitGroup
	errChan := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.handleWebSocketInput(conn); err != nil {
			errChan <- fmt.Errorf("websocket input error: %w", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.handleSSHOutput(conn); err != nil {
			errChan <- fmt.Errorf("ssh output error: %w", err)
		}
	}()

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

// handleWebSocketInput reads from WebSocket and writes to SSH stdin
func (s *SSHSession) handleWebSocketInput(conn *websocket.Conn) error {
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
		}

		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logger.Info("WebSocket closed normally", "server", s.server.Name)
				return nil
			}
			return fmt.Errorf("failed to read from websocket: %w", err)
		}

		if err := s.handleWebSocketMessage(msgType, data); err != nil {
			return err
		}
	}
}

// handleWebSocketMessage processes WebSocket messages (resize or stdin data)
func (s *SSHSession) handleWebSocketMessage(msgType int, data []byte) error {
	if msgType == websocket.TextMessage {
		return s.handleTextMessage(data)
	}

	if msgType == websocket.BinaryMessage {
		if _, err := s.stdin.Write(data); err != nil {
			return fmt.Errorf("failed to write to ssh stdin: %w", err)
		}
	}

	return nil
}

// handleTextMessage processes text WebSocket messages (resize or stdin)
func (s *SSHSession) handleTextMessage(data []byte) error {
	var msg struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}

	if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" {
		if err := s.Resize(msg.Cols, msg.Rows); err != nil {
			logger.Warn("Failed to resize terminal", "error", err)
		}
		return nil
	}

	if _, err := s.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write to ssh stdin: %w", err)
	}

	return nil
}

// handleSSHOutput reads from SSH stdout/stderr and writes to WebSocket
func (s *SSHSession) handleSSHOutput(conn *websocket.Conn) error {
	output := io.MultiReader(s.stdout, s.stderr)
	buf := make([]byte, 32*1024) // 32KB buffer

	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
		}

		n, err := output.Read(buf)
		if err != nil {
			if err == io.EOF {
				logger.Info("SSH output stream closed", "server", s.server.Name)
				return nil
			}
			return fmt.Errorf("failed to read from ssh output: %w", err)
		}

		if n > 0 {
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return fmt.Errorf("failed to write to websocket: %w", err)
			}
		}
	}
}

// Close terminates the SSH session
func (s *SSHSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	logger.Info("Closing SSH session", "server", s.server.Name)

	s.cancel()

	var errors []error

	if s.session != nil {
		if err := s.session.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close session: %w", err))
		}
		s.session = nil
	}

	if s.sshClient != nil {
		if err := s.sshClient.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close SSH client: %w", err))
		}
	}

	s.running = false

	if len(errors) > 0 {
		return fmt.Errorf("errors during close: %v", errors)
	}

	return nil
}
