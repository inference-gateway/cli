package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	tools "github.com/inference-gateway/cli/internal/services/tools"
)

// ScreenshotServer provides an HTTP API for screenshot streaming
type ScreenshotServer struct {
	cfg         *config.Config
	port        int
	server      *http.Server
	buffer      *CircularScreenshotBuffer
	captureCtx  context.Context
	captureStop context.CancelFunc
	mu          sync.RWMutex
	sessionID   string
	imageSvc    domain.ImageService
	running     bool
}

// NewScreenshotServer creates a new screenshot server
func NewScreenshotServer(cfg *config.Config, imageService domain.ImageService, sessionID string) *ScreenshotServer {
	return &ScreenshotServer{
		cfg:       cfg,
		sessionID: sessionID,
		imageSvc:  imageService,
		running:   false,
	}
}

// Start starts the HTTP server and background capture loop
func (s *ScreenshotServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("screenshot server already running")
	}

	logger.Info("Starting screenshot server", "session_id", s.sessionID)

	// Create circular buffer
	bufferSize := s.cfg.ComputerUse.Screenshot.BufferSize
	if bufferSize <= 0 {
		bufferSize = 30 // default
	}

	tempDir := s.cfg.ComputerUse.Screenshot.TempDir
	if tempDir == "" {
		tempDir = "/tmp/infer-screenshots"
	}

	logger.Info("Creating screenshot buffer", "buffer_size", bufferSize, "temp_dir", tempDir)

	buffer, err := NewCircularScreenshotBuffer(bufferSize, tempDir, s.sessionID)
	if err != nil {
		return fmt.Errorf("failed to create screenshot buffer: %w", err)
	}
	s.buffer = buffer

	// Listen on random port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.port = listener.Addr().(*net.TCPAddr).Port
	logger.Info("Screenshot server listening", "port", s.port)

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/screenshots/latest", s.handleGetLatest)
	mux.HandleFunc("/api/screenshots", s.handleGetRecent)
	mux.HandleFunc("/api/screenshots/status", s.handleGetStatus)

	s.server = &http.Server{
		Handler: mux,
	}

	// Start HTTP server in goroutine
	go func() {
		logger.Info("Screenshot HTTP server started", "port", s.port)
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Error("Screenshot server error", "error", err)
		}
	}()

	// Start capture loop
	s.captureCtx, s.captureStop = context.WithCancel(context.Background())
	go s.startCaptureLoop()

	s.running = true
	logger.Info("Screenshot server fully initialized", "port", s.port, "capture_interval", s.cfg.ComputerUse.Screenshot.CaptureInterval)

	return nil
}

// Stop stops the HTTP server and capture loop
func (s *ScreenshotServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	// Stop capture loop
	if s.captureStop != nil {
		s.captureStop()
	}

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
	}

	// Cleanup buffer
	if s.buffer != nil {
		if err := s.buffer.Cleanup(); err != nil {
			logger.Warn("Failed to cleanup buffer", "error", err)
		}
	}

	s.running = false

	return nil
}

// Port returns the port the server is listening on
func (s *ScreenshotServer) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.port
}

// startCaptureLoop runs the background screenshot capture loop
func (s *ScreenshotServer) startCaptureLoop() {
	interval := s.cfg.ComputerUse.Screenshot.CaptureInterval
	if interval <= 0 {
		interval = 3 // default: 3 seconds
	}

	logger.Info("Screenshot capture loop started", "interval_seconds", interval)

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.captureCtx.Done():
			logger.Info("Screenshot capture loop stopped")
			return
		case <-ticker.C:
			logger.Info("Attempting screenshot capture")
			if err := s.captureScreenshot(); err != nil {
				logger.Warn("Screenshot capture failed", "error", err)
			} else {
				logger.Info("Screenshot captured successfully")
			}
		}
	}
}

// captureScreenshot captures a screenshot and adds it to the buffer
func (s *ScreenshotServer) captureScreenshot() error {
	// Use the screenshot tool to capture
	tool := tools.NewScreenshotTool(s.cfg, s.imageSvc, nil) // No rate limiter for auto-capture

	// Execute with default args (full screen)
	result, err := tool.Execute(s.captureCtx, map[string]any{})
	if err != nil {
		return err
	}

	if !result.Success {
		return fmt.Errorf("screenshot capture failed: %s", result.Error)
	}

	// Extract screenshot data
	toolResult, ok := result.Data.(domain.ScreenshotToolResult)
	if !ok {
		return fmt.Errorf("unexpected result type")
	}

	// Get image attachment
	if len(result.Images) == 0 {
		return fmt.Errorf("no image in result")
	}

	imageAttachment := result.Images[0]

	// Create Screenshot object
	screenshot := &domain.Screenshot{
		Timestamp: time.Now(),
		Data:      imageAttachment.Data,
		Width:     toolResult.Width,
		Height:    toolResult.Height,
		Format:    toolResult.Format,
		Method:    toolResult.Method,
	}

	// Add to buffer
	return s.buffer.Add(screenshot)
}

// handleGetLatest handles GET /api/screenshots/latest
func (s *ScreenshotServer) handleGetLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	screenshot, err := s.buffer.GetLatest()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(screenshot); err != nil {
		logger.Warn("Failed to encode screenshot response", "error", err)
	}
}

// handleGetRecent handles GET /api/screenshots?limit=N
func (s *ScreenshotServer) handleGetRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse limit parameter
	limit := 30 // default
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			if parsedLimit > 0 && parsedLimit <= 100 {
				limit = parsedLimit
			}
		}
	}

	screenshots := s.buffer.GetRecent(limit)

	response := map[string]interface{}{
		"screenshots": screenshots,
		"count":       len(screenshots),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Warn("Failed to encode screenshots response", "error", err)
	}
}

// handleGetStatus handles GET /api/screenshots/status
func (s *ScreenshotServer) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	status := map[string]interface{}{
		"running":      s.running,
		"count":        s.buffer.Count(),
		"interval_sec": s.cfg.ComputerUse.Screenshot.CaptureInterval,
		"port":         s.port,
		"session_id":   s.sessionID,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		logger.Warn("Failed to encode status response", "error", err)
	}
}

// GetLatestScreenshot retrieves the latest screenshot from the buffer
// Implements the ScreenshotProvider interface for use by GetLatestScreenshotTool
func (s *ScreenshotServer) GetLatestScreenshot() (*domain.Screenshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.buffer == nil {
		return nil, fmt.Errorf("screenshot buffer not initialized")
	}

	return s.buffer.GetLatest()
}
