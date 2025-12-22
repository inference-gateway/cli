package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/inference-gateway/cli/internal/logger"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WebSocketHandler handles WebSocket connections for live chat
type WebSocketHandler struct {
	sessionManager *SessionManager
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(sessionManager *SessionManager) *WebSocketHandler {
	return &WebSocketHandler{
		sessionManager: sessionManager,
	}
}

// HandleWebSocket handles WebSocket upgrade and communication
func (h *WebSocketHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	logger.Info("WebSocket handler called", "method", r.Method, "path", r.URL.Path, "headers", r.Header)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Failed to upgrade to WebSocket", "error", err)
		return
	}
	defer h.closeConnection(conn)

	clientID := uuid.New().String()
	logger.Info("WebSocket client connected", "client_id", clientID)

	var sessionID string
	var session *Session

	defer h.cleanupSession(session, sessionID, clientID)

	h.messageLoop(conn, r, &session, &sessionID, clientID)
}

func (h *WebSocketHandler) closeConnection(conn *websocket.Conn) {
	if err := conn.Close(); err != nil {
		logger.Warn("Failed to close WebSocket connection", "error", err)
	}
}

func (h *WebSocketHandler) cleanupSession(session *Session, sessionID, clientID string) {
	if session == nil {
		return
	}

	session.Unsubscribe(clientID)
	logger.Info("WebSocket client disconnected, unsubscribed from session", "client_id", clientID, "session_id", sessionID)

	session.mu.RLock()
	clientCount := len(session.clients)
	session.mu.RUnlock()

	if clientCount == 0 {
		logger.Info("Last client disconnected, closing session", "session_id", sessionID)
		if err := h.sessionManager.CloseSession(sessionID); err != nil {
			logger.Warn("Failed to close session", "session_id", sessionID, "error", err)
		}
	}
}

func (h *WebSocketHandler) messageLoop(conn *websocket.Conn, r *http.Request, session **Session, sessionID *string, clientID string) {
	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			logger.Error("Failed to read WebSocket message", "error", err)
			return
		}

		shouldReturn := h.handleMessage(conn, r, msg, session, sessionID, clientID)
		if shouldReturn {
			return
		}
	}
}

func (h *WebSocketHandler) handleMessage(conn *websocket.Conn, r *http.Request, msg WSMessage, session **Session, sessionID *string, clientID string) bool {
	switch msg.Type {
	case "create_session":
		return h.handleCreateSession(conn, r, msg, session, sessionID, clientID)
	case "join_session":
		return h.handleJoinSession(conn, msg, session, sessionID, clientID)
	case "message":
		return h.handleChatMessage(conn, msg, *session)
	case "interrupt":
		return h.handleInterruptMessage(conn, *session)
	case "close_session":
		return h.handleCloseSession(*sessionID)
	default:
		h.sendError(conn, fmt.Sprintf("Unknown message type: %s", msg.Type))
		return false
	}
}

func (h *WebSocketHandler) handleCreateSession(conn *websocket.Conn, r *http.Request, msg WSMessage, session **Session, sessionID *string, clientID string) bool {
	requestedSessionID := msg.SessionID
	if requestedSessionID == "" {
		requestedSessionID = uuid.New().String()
	}

	conversationID := msg.ConversationID

	logger.Info("=== CREATE_SESSION REQUEST ===",
		"requested_session_id", requestedSessionID,
		"conversation_id", conversationID,
		"client_id", clientID)

	existingSession, exists := h.sessionManager.GetSession(requestedSessionID)
	canReuse := exists && (existingSession.ConversationID == conversationID ||
		(existingSession.ConversationID == "" && conversationID == ""))

	if canReuse {
		*session = h.reuseExistingSession(existingSession, requestedSessionID, conversationID)
		*sessionID = requestedSessionID
	} else {
		newSession, err := h.createNewSession(r, requestedSessionID, conversationID, exists)
		if err != nil {
			h.sendError(conn, fmt.Sprintf("Failed to create session: %v", err))
			return true
		}
		*session = newSession
		*sessionID = requestedSessionID
	}

	outputChan := (*session).Subscribe(clientID)

	h.sendMessage(conn, WSMessage{
		Type:           "session_created",
		SessionID:      *sessionID,
		ConversationID: (*session).ConversationID,
	})

	go h.forwardOutput(conn, outputChan)
	return false
}

func (h *WebSocketHandler) reuseExistingSession(existingSession *Session, requestedSessionID, conversationID string) *Session {
	logger.Info("✅ Reusing existing session with same conversation",
		"session_id", requestedSessionID,
		"conversation_id", conversationID)
	return existingSession
}

func (h *WebSocketHandler) createNewSession(r *http.Request, requestedSessionID, conversationID string, exists bool) (*Session, error) {
	if exists {
		if err := h.sessionManager.CloseSession(requestedSessionID); err != nil {
			logger.Warn("Failed to close existing session", "session_id", requestedSessionID, "error", err)
		}
	}

	return h.sessionManager.CreateSession(r.Context(), requestedSessionID, conversationID)
}

func (h *WebSocketHandler) handleJoinSession(conn *websocket.Conn, msg WSMessage, session **Session, sessionID *string, clientID string) bool {
	if msg.SessionID == "" {
		h.sendError(conn, "Session ID required")
		return false
	}

	var exists bool
	*session, exists = h.sessionManager.GetSession(msg.SessionID)
	if !exists {
		h.sendError(conn, fmt.Sprintf("Session %s not found", msg.SessionID))
		return false
	}

	*sessionID = msg.SessionID

	outputChan := (*session).Subscribe(clientID)

	h.sendMessage(conn, WSMessage{
		Type:      "session_joined",
		SessionID: *sessionID,
	})

	go h.forwardOutput(conn, outputChan)
	return false
}

func (h *WebSocketHandler) handleChatMessage(conn *websocket.Conn, msg WSMessage, session *Session) bool {
	if session == nil {
		h.sendError(conn, "No active session")
		return false
	}

	if err := session.SendMessage(msg.Content, msg.Images, msg.Model); err != nil {
		h.sendError(conn, fmt.Sprintf("Failed to send message: %v", err))
	}
	return false
}

func (h *WebSocketHandler) handleInterruptMessage(conn *websocket.Conn, session *Session) bool {
	if session == nil {
		h.sendError(conn, "No active session")
		return false
	}

	if err := session.SendInterrupt(); err != nil {
		h.sendError(conn, fmt.Sprintf("Failed to send interrupt: %v", err))
	}
	return false
}

func (h *WebSocketHandler) handleCloseSession(sessionID string) bool {
	if sessionID != "" {
		if err := h.sessionManager.CloseSession(sessionID); err != nil {
			logger.Warn("Failed to close session", "session_id", sessionID, "error", err)
		}
	}
	return true
}

// forwardOutput forwards session output to WebSocket client
func (h *WebSocketHandler) forwardOutput(conn *websocket.Conn, outputChan chan []byte) {
	for data := range outputChan {
		var jsonTest map[string]any
		if err := json.Unmarshal(data, &jsonTest); err != nil {
			wrapped := WSMessage{
				Type:    "output",
				Content: string(data),
				Time:    time.Now().UTC().Format(time.RFC3339),
			}
			if err := conn.WriteJSON(wrapped); err != nil {
				logger.Error("Failed to write wrapped message to WebSocket", "error", err)
				return
			}
		} else {
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				logger.Error("Failed to write to WebSocket", "error", err)
				return
			}
		}
	}
}

// sendMessage sends a message to the WebSocket client
func (h *WebSocketHandler) sendMessage(conn *websocket.Conn, msg WSMessage) {
	if err := conn.WriteJSON(msg); err != nil {
		logger.Error("Failed to send WebSocket message", "error", err)
	}
}

// sendError sends an error message to the client
func (h *WebSocketHandler) sendError(conn *websocket.Conn, errMsg string) {
	msg := WSMessage{
		Type:  "error",
		Error: errMsg,
		Time:  time.Now().UTC().Format(time.RFC3339),
	}
	h.sendMessage(conn, msg)
}

// WSMessage represents a WebSocket message
type WSMessage struct {
	Type           string `json:"type"`
	SessionID      string `json:"session_id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Content        string `json:"content,omitempty"`
	Images         []any  `json:"images,omitempty"`
	Model          string `json:"model,omitempty"`
	Error          string `json:"error,omitempty"`
	Time           string `json:"time,omitempty"`
	Data           any    `json:"data,omitempty"`
}
