package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/logger"
)

// SessionManager manages headless CLI sessions
type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// Session represents a headless CLI session
type Session struct {
	ID             string
	ConversationID string
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	stderr         io.ReadCloser
	cancel         context.CancelFunc
	clients        map[string]chan []byte
	mu             sync.RWMutex
	wg             sync.WaitGroup
}

// NewSessionManager creates a new session manager
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// CreateSession spawns a new headless CLI process
func (sm *SessionManager) CreateSession(ctx context.Context, sessionID string, conversationID string) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.sessions[sessionID]; exists {
		return nil, fmt.Errorf("session %s already exists", sessionID)
	}

	sessionCtx, cancel := context.WithCancel(ctx)

	execPath, err := os.Executable()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get executable path: %w", err)
	}

	args := []string{"chat", "--headless", "--session-id", sessionID}
	if conversationID != "" {
		args = append(args, "--conversation-id", conversationID)
	}

	cmd := exec.CommandContext(sessionCtx, execPath, args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start headless process: %w", err)
	}

	session := &Session{
		ID:             sessionID,
		ConversationID: conversationID,
		cmd:            cmd,
		stdin:          stdin,
		stdout:         stdout,
		stderr:         stderr,
		cancel:         cancel,
		clients:        make(map[string]chan []byte),
	}

	sm.sessions[sessionID] = session

	session.wg.Add(2)
	go session.readOutput()
	go session.readStderr()

	return session, nil
}

// GetSession retrieves an existing session
func (sm *SessionManager) GetSession(sessionID string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	session, exists := sm.sessions[sessionID]
	return session, exists
}

// CloseSession terminates a session
func (sm *SessionManager) CloseSession(sessionID string) error {
	sm.mu.Lock()
	session, exists := sm.sessions[sessionID]
	if !exists {
		sm.mu.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}

	delete(sm.sessions, sessionID)
	sm.mu.Unlock()

	logger.Info("🛑 SESSION CLOSING", "session_id", sessionID, "conversation_id", session.ConversationID, "pid", session.cmd.Process.Pid)

	shutdownInput := map[string]any{
		"type": "shutdown",
	}
	if data, err := json.Marshal(shutdownInput); err == nil {
		data = append(data, '\n')

		session.mu.Lock()
		_, writeErr := session.stdin.Write(data)
		session.mu.Unlock()

		if writeErr != nil {
			logger.Warn("Failed to send shutdown signal", "session_id", sessionID, "error", writeErr)
		} else {
			logger.Info("Sent shutdown signal to session", "session_id", sessionID)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- session.cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			logger.Info("Headless process exited with error", "session_id", sessionID, "error", err)
		} else {
			logger.Info("Headless process exited cleanly", "session_id", sessionID)
		}
	case <-time.After(5 * time.Second):
		logger.Warn("Headless process did not exit gracefully, forcing termination", "session_id", sessionID)
		session.cancel()
		<-done
	}

	if err := session.stdout.Close(); err != nil {
		logger.Warn("Failed to close stdout", "error", err)
	}
	if err := session.stderr.Close(); err != nil {
		logger.Warn("Failed to close stderr", "error", err)
	}

	session.wg.Wait()

	if err := session.stdin.Close(); err != nil {
		logger.Warn("Failed to close stdin", "error", err)
	}
	session.mu.Lock()
	for _, ch := range session.clients {
		close(ch)
	}
	session.mu.Unlock()

	logger.Info("Session cleanup completed", "session_id", sessionID)
	return nil
}

// readOutput continuously reads from the CLI stdout and broadcasts to clients
func (s *Session) readOutput() {
	defer s.wg.Done()
	scanner := bufio.NewScanner(s.stdout)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 1MB initial, 10MB max

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		s.mu.RLock()
		for clientID, ch := range s.clients {
			select {
			case ch <- line:
			default:
				logger.Warn("Client channel full, skipping", "session_id", s.ID, "client_id", clientID)
			}
		}
		s.mu.RUnlock()
	}

	if err := scanner.Err(); err != nil {
		logger.Error("Error reading session output", "session_id", s.ID, "error", err)
	}
}

// readStderr continuously reads from the CLI stderr (for debugging)
func (s *Session) readStderr() {
	defer s.wg.Done()
	scanner := bufio.NewScanner(s.stderr)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024) // 1MB initial, 10MB max

	for scanner.Scan() {
	}

	if err := scanner.Err(); err != nil {
		logger.Error("Error reading session stderr", "session_id", s.ID, "error", err)
	}
}

// Subscribe adds a client to receive session output
func (s *Session) Subscribe(clientID string) chan []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch := make(chan []byte, 100)
	s.clients[clientID] = ch
	return ch
}

// Unsubscribe removes a client
func (s *Session) Unsubscribe(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ch, exists := s.clients[clientID]
	if !exists {
		return
	}

	delete(s.clients, clientID)

	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Debug("Channel already closed during unsubscribe", "session_id", s.ID, "client_id", clientID)
			}
		}()
		close(ch)
	}()
}

// SendMessage sends a message to the headless CLI
func (s *Session) SendMessage(content string, images []any, model string) error {
	input := map[string]any{
		"type":    "message",
		"content": content,
		"images":  images,
	}

	if model != "" {
		input["model"] = model
	}

	data, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("failed to marshal input: %w", err)
	}

	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}

// SendInterrupt sends an interrupt signal to the headless CLI
func (s *Session) SendInterrupt() error {
	input := map[string]any{
		"type": "interrupt",
	}

	data, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("failed to marshal input: %w", err)
	}

	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	return nil
}
