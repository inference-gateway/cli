package web

import (
	"sync"
	"time"

	websocket "github.com/gorilla/websocket"
	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// SessionManager tracks and manages all active sessions
type SessionManager struct {
	cfg      *config.Config
	viper    *viper.Viper
	sessions map[string]*SessionEntry
	mu       sync.RWMutex
	done     chan struct{}
}

// SessionEntry tracks a session and its activity
type SessionEntry struct {
	session        Session
	lastActive     time.Time
	screenshotPort int // Local forwarded port for screenshot streaming
	mu             sync.Mutex
}

func NewSessionManager(cfg *config.Config, v *viper.Viper) *SessionManager {
	sm := &SessionManager{
		cfg:      cfg,
		viper:    v,
		sessions: make(map[string]*SessionEntry),
		done:     make(chan struct{}),
	}

	go sm.cleanupLoop()

	return sm
}

// CreateSession creates a new session and registers it
func (sm *SessionManager) CreateSession(sessionID string) Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session := NewLocalPTYSession(sm.cfg, sm.viper)
	entry := &SessionEntry{
		session:    session,
		lastActive: time.Now(),
	}

	sm.sessions[sessionID] = entry
	logger.Info("Session created", "id", sessionID, "total", len(sm.sessions))

	return session
}

// UpdateActivity updates the last activity time for a session
func (sm *SessionManager) UpdateActivity(sessionID string) {
	sm.mu.RLock()
	entry, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if exists {
		entry.mu.Lock()
		entry.lastActive = time.Now()
		entry.mu.Unlock()
	}
}

// RemoveSession removes and stops a session
func (sm *SessionManager) RemoveSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if entry, exists := sm.sessions[sessionID]; exists {
		if err := entry.session.Close(); err != nil {
			logger.Warn("Error stopping session", "id", sessionID, "error", err)
		}
		delete(sm.sessions, sessionID)
		logger.Info("Session removed", "id", sessionID, "total", len(sm.sessions))
	}
}

// cleanupLoop periodically cleans up inactive sessions
func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-sm.done:
			return
		case <-ticker.C:
			sm.cleanupInactiveSessions()
		}
	}
}

// cleanupInactiveSessions removes sessions inactive for more than the configured threshold
func (sm *SessionManager) cleanupInactiveSessions() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	inactiveThreshold := time.Duration(sm.cfg.Web.SessionInactivityMins) * time.Minute
	now := time.Now()
	var toRemove []string

	for sessionID, entry := range sm.sessions {
		entry.mu.Lock()
		inactive := now.Sub(entry.lastActive)
		entry.mu.Unlock()

		if inactive > inactiveThreshold {
			toRemove = append(toRemove, sessionID)
		}
	}

	for _, sessionID := range toRemove {
		if entry, exists := sm.sessions[sessionID]; exists {
			logger.Info("Cleaning up inactive session", "id", sessionID, "inactive_duration", now.Sub(entry.lastActive), "threshold", inactiveThreshold)
			if err := entry.session.Close(); err != nil {
				logger.Warn("Error stopping inactive session", "id", sessionID, "error", err)
			}
			delete(sm.sessions, sessionID)
		}
	}

	if len(toRemove) > 0 {
		logger.Info("Inactive sessions cleaned up", "count", len(toRemove), "remaining", len(sm.sessions), "threshold", inactiveThreshold)
	}
}

// ActiveSessionCount returns the number of currently active sessions
func (sm *SessionManager) ActiveSessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// RegisterSession registers an existing session with the manager
func (sm *SessionManager) RegisterSession(sessionID string, session Session) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	entry := &SessionEntry{
		session:    session,
		lastActive: time.Now(),
	}

	sm.sessions[sessionID] = entry
	logger.Info("Session registered", "id", sessionID, "total", len(sm.sessions))
}

// SetScreenshotPort sets the local screenshot port for a session
func (sm *SessionManager) SetScreenshotPort(sessionID string, port int) {
	sm.mu.RLock()
	entry, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if exists {
		entry.mu.Lock()
		entry.screenshotPort = port
		entry.mu.Unlock()
		logger.Info("Screenshot port set for session",
			"session_id", sessionID,
			"port", port)
	} else {
		logger.Warn("Cannot set screenshot port: session not found",
			"session_id", sessionID,
			"port", port)
	}
}

// GetScreenshotPort retrieves the local screenshot port for a session
func (sm *SessionManager) GetScreenshotPort(sessionID string) (int, bool) {
	sm.mu.RLock()
	entry, exists := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !exists {
		logger.Warn("Cannot get screenshot port: session not found",
			"session_id", sessionID,
			"total_sessions", len(sm.sessions))
		return 0, false
	}

	entry.mu.Lock()
	port := entry.screenshotPort
	entry.mu.Unlock()

	if port == 0 {
		logger.Warn("Screenshot port not set for session",
			"session_id", sessionID)
		return 0, false
	}

	logger.Info("Retrieved screenshot port for session",
		"session_id", sessionID,
		"port", port)

	return port, true
}

// Shutdown stops all sessions and the cleanup goroutine
func (sm *SessionManager) Shutdown() {
	close(sm.done)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	logger.Info("Shutting down session manager", "active_sessions", len(sm.sessions))

	for sessionID, entry := range sm.sessions {
		logger.Info("Stopping session", "id", sessionID)
		if err := entry.session.Close(); err != nil {
			logger.Warn("Error stopping session during shutdown", "id", sessionID, "error", err)
		}
	}

	sm.sessions = make(map[string]*SessionEntry)
	logger.Info("All sessions stopped")
}

// SessionWrapper wraps a session to track activity
type SessionWrapper struct {
	sessionID string
	session   Session
	manager   *SessionManager
}

func (sm *SessionManager) WrapSession(sessionID string, session Session) *SessionWrapper {
	return &SessionWrapper{
		sessionID: sessionID,
		session:   session,
		manager:   sm,
	}
}

func (sh *SessionWrapper) HandleConnection(conn *websocket.Conn) error {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sh.manager.UpdateActivity(sh.sessionID)
			}
		}
	}()

	err := sh.session.HandleConnection(conn)

	close(done)

	return err
}
