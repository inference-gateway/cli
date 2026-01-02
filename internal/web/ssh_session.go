package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	websocket "github.com/gorilla/websocket"
	ssh "golang.org/x/crypto/ssh"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// SSHSession wraps an SSH session with PTY for remote terminal access
type SSHSession struct {
	sshClient           *SSHClient
	server              *config.SSHServerConfig
	gatewayURL          string
	session             *ssh.Session
	stdin               io.WriteCloser
	stdout              io.Reader
	stderr              io.Reader
	mu                  sync.Mutex
	running             bool
	ctx                 context.Context
	cancel              context.CancelFunc
	ws                  *websocket.Conn
	tunnelListener      net.Listener
	tunnelCtx           context.Context
	tunnelCancel        context.CancelFunc
	tunnelWg            sync.WaitGroup // Track active forwarding goroutines
	screenshotPort      int
	localScreenshotPort int
	sessionID           string
	sessionManager      *SessionManager
}

// NewSSHSession creates a new SSH session with PTY
func NewSSHSession(client *SSHClient, server *config.SSHServerConfig, gatewayURL string, sessionID string, sessionManager *SessionManager) (*SSHSession, error) {
	if client == nil {
		return nil, fmt.Errorf("SSH client is required")
	}
	if server == nil {
		return nil, fmt.Errorf("server configuration is required")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &SSHSession{
		sshClient:      client,
		server:         server,
		gatewayURL:     gatewayURL,
		ctx:            ctx,
		cancel:         cancel,
		sessionID:      sessionID,
		sessionManager: sessionManager,
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

	// Source /etc/environment to pick up docker-compose environment variables, then run infer
	// Redirect stderr to /dev/null to suppress X11 library warnings
	cmd := fmt.Sprintf(
		"sh -c 'set -a; test -f /etc/environment && . /etc/environment; set +a; "+
			"INFER_GATEWAY_URL=%s INFER_GATEWAY_MODE=remote "+
			"%s %s 2>/dev/null'",
		s.gatewayURL, commandPath, strings.Join(cmdArgs, " "))

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
	// Store WebSocket connection for later use
	s.mu.Lock()
	s.ws = conn
	s.mu.Unlock()

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
			s.handleScreenshotPortEscape(buf[:n])

			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return fmt.Errorf("failed to write to websocket: %w", err)
			}
		}
	}
}

// handleScreenshotPortEscape checks for and handles screenshot port escape sequences
func (s *SSHSession) handleScreenshotPortEscape(data []byte) {
	port, found := extractPortFromEscape(data)
	if !found {
		return
	}

	logger.Info("Detected screenshot port from remote CLI",
		"port", port,
		"server", s.server.Name)

	localPort, err := s.SetupPortForwarding(port)
	if err != nil {
		logger.Error("Failed to set up port forwarding", "error", err)
		return
	}

	s.notifyWebUI(localPort)
}

// SetupPortForwarding sets up SSH port forwarding from local to remote port
func (s *SSHSession) SetupPortForwarding(remotePort int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("failed to listen on local port: %w", err)
	}

	localPort := listener.Addr().(*net.TCPAddr).Port
	s.tunnelListener = listener
	s.screenshotPort = remotePort
	s.localScreenshotPort = localPort

	// Create context for tunnel management
	s.tunnelCtx, s.tunnelCancel = context.WithCancel(context.Background())

	logger.Info("Setting up port forwarding",
		"local_port", localPort,
		"remote_port", remotePort,
		"server", s.server.Name,
		"session_id", s.sessionID)

	if s.sessionManager != nil {
		s.sessionManager.SetScreenshotPort(s.sessionID, localPort)
		logger.Info("Registered screenshot port with session manager",
			"session_id", s.sessionID,
			"local_port", localPort)
	}

	go s.forwardConnections(listener, remotePort)

	return localPort, nil
}

// forwardConnections handles port forwarding for incoming connections
func (s *SSHSession) forwardConnections(listener net.Listener, remotePort int) {
	defer func() {
		if err := listener.Close(); err != nil {
			logger.Warn("Failed to close listener", "error", err)
		}
	}()

	for {
		select {
		case <-s.tunnelCtx.Done():
			logger.Info("Port forwarding stopped", "server", s.server.Name)
			return
		default:
		}

		localConn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.tunnelCtx.Done():
				return
			default:
				logger.Error("Failed to accept local connection", "error", err)
				continue
			}
		}

		s.tunnelWg.Add(1)
		go s.handleForwardedConnection(localConn, remotePort)
	}
}

// handleForwardedConnection forwards a single connection through SSH
func (s *SSHSession) handleForwardedConnection(localConn net.Conn, remotePort int) {
	defer s.tunnelWg.Done()
	defer func() {
		if err := localConn.Close(); err != nil {
			logger.Debug("Failed to close local connection", "error", err)
		}
	}()

	remoteAddr := fmt.Sprintf("localhost:%d", remotePort)
	remoteConn, err := s.sshClient.client.Dial("tcp", remoteAddr)
	if err != nil {
		logger.Error("Failed to dial remote address through SSH",
			"remote_addr", remoteAddr,
			"error", err)
		return
	}
	defer func() {
		if err := remoteConn.Close(); err != nil {
			logger.Debug("Failed to close remote connection", "error", err)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if _, err := io.Copy(remoteConn, localConn); err != nil {
			logger.Debug("Copy from local to remote ended", "error", err)
		}
	}()

	go func() {
		defer wg.Done()
		if _, err := io.Copy(localConn, remoteConn); err != nil {
			logger.Debug("Copy from remote to local ended", "error", err)
		}
	}()

	wg.Wait()
}

// notifyWebUI sends a message to the WebSocket with the local screenshot port
func (s *SSHSession) notifyWebUI(localPort int) {
	if s.ws == nil {
		logger.Warn("WebSocket connection not available for notification")
		return
	}

	msg := map[string]interface{}{
		"type": "screenshot_port",
		"port": localPort,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		logger.Error("Failed to marshal screenshot port message", "error", err)
		return
	}

	if err := s.ws.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Error("Failed to send screenshot port to WebSocket", "error", err)
		return
	}

	logger.Info("Notified WebUI of screenshot port", "local_port", localPort)
}

// extractPortFromEscape extracts port number from escape sequence
func extractPortFromEscape(data []byte) (int, bool) {
	startSeq := []byte("\x1b]5555;screenshot_port=")
	endSeq := []byte("\x07")

	startIdx := bytes.Index(data, startSeq)
	if startIdx == -1 {
		return 0, false
	}

	searchStart := startIdx + len(startSeq)
	endIdx := bytes.Index(data[searchStart:], endSeq)
	if endIdx == -1 {
		return 0, false
	}

	portStr := string(data[searchStart : searchStart+endIdx])
	port, err := strconv.Atoi(portStr)
	if err != nil {
		logger.Warn("Failed to parse port from escape sequence", "port_str", portStr, "error", err)
		return 0, false
	}

	return port, true
}

// Close terminates the SSH session
func (s *SSHSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	logger.Info("Closing SSH session", "server", s.server.Name)

	s.cancel()

	var errors []error

	if s.tunnelCancel != nil {
		s.tunnelCancel()
		s.tunnelCancel = nil
	}

	if s.tunnelListener != nil {
		if err := s.tunnelListener.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close tunnel listener: %w", err))
		}
		s.tunnelListener = nil
	}

	logger.Info("Waiting for active port forwarding connections to close", "server", s.server.Name)
	s.tunnelWg.Wait()
	logger.Info("All port forwarding connections closed", "server", s.server.Name)

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
