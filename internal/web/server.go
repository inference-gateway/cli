package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	uuid "github.com/google/uuid"
	websocket "github.com/gorilla/websocket"
	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

//go:embed static/*
var staticFiles embed.FS

//go:embed templates/*
var templates embed.FS

type WebTerminalServer struct {
	cfg            *config.Config
	viper          *viper.Viper
	server         *http.Server
	upgrader       websocket.Upgrader
	sessionManager *SessionManager
}

func NewWebTerminalServer(cfg *config.Config, v *viper.Viper) *WebTerminalServer {
	return &WebTerminalServer{
		cfg:   cfg,
		viper: v,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
}

func (s *WebTerminalServer) Start() error {
	s.sessionManager = NewSessionManager(s.cfg, s.viper)

	logger.Info("Checking embedded static files...")
	if err := fs.WalkDir(staticFiles, ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			logger.Info("Embedded file found", "path", path)
		}
		return nil
	}); err != nil {
		logger.Warn("Failed to walk static files", "error", err)
	}

	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("failed to create static filesystem: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/api/servers", s.handleServers)
	mux.HandleFunc("/ws", s.handleWebSocket)

	addr := fmt.Sprintf("%s:%d", s.cfg.Web.Host, s.cfg.Web.Port)
	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go s.handleShutdown()

	logger.Info("Web terminal server started", "url", fmt.Sprintf("http://%s", addr))
	fmt.Printf("\n🌐 Web terminal available at: http://%s\n", addr)
	fmt.Printf("   Open this URL in your browser to access the terminal.\n\n")

	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func (s *WebTerminalServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(templates, "templates/index.html")
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		logger.Error("Failed to parse template", "error", err)
		return
	}

	data := struct {
		Title  string
		WSPort int
	}{
		Title:  "Inference Gateway CLI",
		WSPort: s.cfg.Web.Port,
	}

	if err := tmpl.Execute(w, data); err != nil {
		logger.Error("Failed to execute template", "error", err)
	}
}

func (s *WebTerminalServer) handleServers(w http.ResponseWriter, r *http.Request) {
	type ServerInfo struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}

	servers := []ServerInfo{}

	// Add local mode option
	servers = append(servers, ServerInfo{
		ID:          "local",
		Name:        "Local",
		Description: "Run infer chat locally on this machine",
		Tags:        []string{"local"},
	})

	// Add configured remote servers
	for _, srv := range s.cfg.Web.Servers {
		servers = append(servers, ServerInfo{
			ID:          srv.ID,
			Name:        srv.Name,
			Description: srv.Description,
			Tags:        srv.Tags,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"servers":     servers,
		"ssh_enabled": s.cfg.Web.SSH.Enabled,
	}); err != nil {
		logger.Error("Failed to encode servers response", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *WebTerminalServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("WebSocket upgrade failed", "error", err)
		return
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Warn("Failed to close WebSocket connection", "error", err)
		}
	}()

	sessionID := uuid.New().String()
	logger.Info("WebSocket connected", "remote", r.RemoteAddr, "session_id", sessionID)

	// Wait for initial connection message with server selection
	cols, rows := 80, 24
	serverID := "local" // Default to local mode

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		logger.Warn("Failed to set read deadline", "session_id", sessionID, "error", err)
	}
	msgType, data, err := conn.ReadMessage()
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		logger.Warn("Failed to clear read deadline", "session_id", sessionID, "error", err)
	}

	if err == nil && msgType == websocket.TextMessage {
		var msg struct {
			Type     string `json:"type"`
			ServerID string `json:"server_id"`
			Cols     int    `json:"cols"`
			Rows     int    `json:"rows"`
		}
		if json.Unmarshal(data, &msg) == nil && msg.Type == "init" {
			cols, rows = msg.Cols, msg.Rows
			serverID = msg.ServerID
			logger.Info("Session initialized",
				"session_id", sessionID,
				"server_id", serverID,
				"cols", cols,
				"rows", rows)
		}
	} else if err != nil {
		logger.Warn("Failed to read init message, using defaults",
			"session_id", sessionID, "error", err)
	}

	serverCfg, ok := s.findServerConfig(serverID, sessionID, conn)
	if !ok {
		return
	}

	handler, err := CreateSessionHandler(&s.cfg.Web, serverCfg, s.cfg, s.viper)
	if err != nil {
		logger.Error("Failed to create session",
			"error", err,
			"server_id", serverID)
		errMsg := fmt.Sprintf("Failed to start session: %v", err)
		if writeErr := conn.WriteMessage(websocket.TextMessage, []byte(errMsg)); writeErr != nil {
			logger.Warn("Failed to write error message", "session_id", sessionID, "error", writeErr)
		}
		return
	}
	defer func() {
		if closeErr := handler.Close(); closeErr != nil {
			logger.Warn("Failed to close session handler", "session_id", sessionID, "error", closeErr)
		}
	}()

	if err := handler.Start(cols, rows); err != nil {
		logger.Error("Failed to start session", "error", err)
		errMsg := fmt.Sprintf("Failed to start terminal: %v", err)
		if writeErr := conn.WriteMessage(websocket.TextMessage, []byte(errMsg)); writeErr != nil {
			logger.Warn("Failed to write error message", "session_id", sessionID, "error", writeErr)
		}
		return
	}

	logger.Info("Session started", "session_id", sessionID, "server_id", serverID)

	// Handle I/O
	if err := handler.HandleConnection(conn); err != nil {
		logger.Error("Connection error", "session_id", sessionID, "error", err)
	}

	logger.Info("WebSocket connection closed", "session_id", sessionID)
}

// findServerConfig finds server configuration by ID and handles error reporting
func (s *WebTerminalServer) findServerConfig(serverID, sessionID string, conn *websocket.Conn) (*config.SSHServerConfig, bool) {
	if serverID == "local" {
		return nil, true
	}

	for i := range s.cfg.Web.Servers {
		if s.cfg.Web.Servers[i].ID == serverID {
			return &s.cfg.Web.Servers[i], true
		}
	}

	errMsg := fmt.Sprintf("Server not found: %s", serverID)
	logger.Error("Invalid server ID", "session_id", sessionID, "server_id", serverID)
	if writeErr := conn.WriteMessage(websocket.TextMessage, []byte(errMsg)); writeErr != nil {
		logger.Warn("Failed to write error message", "session_id", sessionID, "error", writeErr)
	}
	return nil, false
}

func (s *WebTerminalServer) handleShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n\nShutting down web terminal server...")
	logger.Info("Shutting down web terminal server...")

	if s.sessionManager != nil {
		activeSessions := s.sessionManager.ActiveSessionCount()
		if activeSessions > 0 {
			fmt.Printf("Stopping %d active session(s)...\n", activeSessions)
		}
		s.sessionManager.Shutdown()
		fmt.Println("All sessions stopped")
	}

	fmt.Println("Stopping HTTP server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		logger.Error("Server shutdown error", "error", err)
		fmt.Printf("Server shutdown error: %v\n", err)
	}

	logger.Info("Web terminal server stopped")
	fmt.Println("Web terminal server stopped gracefully")
	fmt.Println()
}
