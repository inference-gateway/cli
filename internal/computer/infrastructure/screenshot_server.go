package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	"image"
	"image/jpeg"
	_ "image/png"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"

	_ "github.com/inference-gateway/cli/internal/computer/infrastructure/display/wayland"
	_ "github.com/inference-gateway/cli/internal/computer/infrastructure/display/x11"
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

	bufferSize := s.cfg.ComputerUse.Screenshot.BufferSize
	if bufferSize <= 0 {
		bufferSize = 30
	}

	tempDir := s.cfg.ComputerUse.Screenshot.TempDir
	if tempDir == "" {
		tempDir = filepath.Join(s.cfg.GetConfigDir(), "tmp", "screenshots")
	}

	absTempDir, err := filepath.Abs(tempDir)
	if err != nil {
		return fmt.Errorf("failed to resolve temp directory path: %w", err)
	}

	buffer, err := NewCircularScreenshotBuffer(bufferSize, absTempDir, s.sessionID)
	if err != nil {
		return fmt.Errorf("failed to create screenshot buffer: %w", err)
	}
	s.buffer = buffer

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.port = listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc("/api/screenshots/latest", s.handleGetLatest)
	mux.HandleFunc("/api/screenshots", s.handleGetRecent)
	mux.HandleFunc("/api/screenshots/status", s.handleGetStatus)

	s.server = &http.Server{
		Handler: mux,
	}

	go func() {
		if err := s.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Error("screenshot server error", "error", err)
		}
	}()

	s.captureCtx, s.captureStop = context.WithCancel(context.Background())
	go s.startCaptureLoop()

	s.running = true

	return nil
}

// Stop stops the HTTP server and capture loop
func (s *ScreenshotServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	if s.captureStop != nil {
		s.captureStop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
	}

	if s.buffer != nil {
		if err := s.buffer.Cleanup(); err != nil {
			logger.Warn("failed to cleanup buffer", "error", err)
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
		interval = 3
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	if err := s.captureScreenshot(); err != nil {
		logger.Warn("screenshot capture failed", "error", err)
	}

	for {
		select {
		case <-s.captureCtx.Done():
			return
		case <-ticker.C:
			if err := s.captureScreenshot(); err != nil {
				logger.Warn("screenshot capture failed", "error", err)
			}
		}
	}
}

// captureScreenshot captures a screenshot and adds it to the buffer
func (s *ScreenshotServer) captureScreenshot() error {
	displayProvider, err := display.DetectDisplay()
	if err != nil {
		return fmt.Errorf("no compatible display platform detected: %w", err)
	}

	controller, err := displayProvider.GetController()
	if err != nil {
		return fmt.Errorf("failed to get platform controller: %w", err)
	}
	defer func() {
		if closeErr := controller.Close(); closeErr != nil {
			logger.Warn("failed to close controller", "error", closeErr)
		}
	}()

	width, height, err := controller.GetScreenDimensions(s.captureCtx)
	if err != nil {
		return fmt.Errorf("failed to get screen dimensions: %w", err)
	}

	imageBytes, err := controller.CaptureScreenBytes(s.captureCtx, nil)
	if err != nil {
		return fmt.Errorf("failed to capture screenshot: %w", err)
	}

	imgConfig, _, err := image.DecodeConfig(bytes.NewReader(imageBytes))
	if err != nil {
		logger.Warn("failed to decode image config, using controller dimensions", "error", err)
	} else {
		actualWidth := imgConfig.Width
		actualHeight := imgConfig.Height

		if actualWidth != width || actualHeight != height {
			width = actualWidth
			height = actualHeight
		}
	}

	img, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return fmt.Errorf("failed to decode screenshot: %w", err)
	}

	logicalWidth, logicalHeight, err := controller.GetScreenDimensions(s.captureCtx)
	if err != nil {
		logger.Warn("failed to get logical dimensions", "error", err)
		logicalWidth, logicalHeight = width, height
	}

	originalWidth := logicalWidth
	originalHeight := logicalHeight

	fitW, fitH := s.cfg.ComputerUse.Screenshot.FitDims(logicalWidth, logicalHeight)
	if width != fitW || height != fitH {
		img = display.ResizeImage(img, fitW, fitH)
		width = fitW
		height = fitH
	}

	quality := s.cfg.ComputerUse.Screenshot.Quality
	if quality <= 0 || quality > 100 {
		quality = 60
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return fmt.Errorf("failed to encode JPEG: %w", err)
	}
	imageBytes = buf.Bytes()

	imageAttachment, err := s.imageSvc.ReadImageFromBinary(imageBytes, "screenshot.jpeg")
	if err != nil {
		return fmt.Errorf("failed to process image: %w", err)
	}

	screenshot := &agentdomain.Frame{
		Timestamp:      time.Now(),
		Data:           imageAttachment.Data,
		Width:          width,
		Height:         height,
		Format:         s.cfg.ComputerUse.Screenshot.Format,
		Method:         displayProvider.GetDisplayInfo().Name,
		OriginalWidth:  originalWidth,
		OriginalHeight: originalHeight,
	}

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
		logger.Warn("failed to encode screenshot response", "error", err)
	}
}

// handleGetRecent handles GET /api/screenshots?limit=N
func (s *ScreenshotServer) handleGetRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	limit := 30
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil {
			if parsedLimit > 0 && parsedLimit <= 100 {
				limit = parsedLimit
			}
		}
	}

	screenshots := s.buffer.GetRecent(limit)

	response := map[string]any{
		"screenshots": screenshots,
		"count":       len(screenshots),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Warn("failed to encode screenshots response", "error", err)
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

	status := map[string]any{
		"running":      s.running,
		"count":        s.buffer.Count(),
		"interval_sec": s.cfg.ComputerUse.Screenshot.CaptureInterval,
		"port":         s.port,
		"session_id":   s.sessionID,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		logger.Warn("failed to encode status response", "error", err)
	}
}

// GetLatestFrame retrieves the latest screenshot from the buffer
func (s *ScreenshotServer) GetLatestFrame() (*agentdomain.Frame, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.buffer == nil {
		return nil, fmt.Errorf("screenshot buffer not initialized")
	}

	return s.buffer.GetLatest()
}
