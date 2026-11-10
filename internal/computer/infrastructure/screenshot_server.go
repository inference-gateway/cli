package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

const (
	defaultCaptureInterval = 3 * time.Second
	// heldCaptureInterval paces the stream while a person drives the screen.
	heldCaptureInterval = 500 * time.Millisecond
	// freshFrameWait bounds how long a reader waits for the frame an input owes.
	freshFrameWait = time.Second
)

// ScreenshotServer provides an HTTP API for screenshot streaming. While a
// person holds the screen it captures faster and right after each input, so
// the view they drive follows what they do.
type ScreenshotServer struct {
	cfg         *config.Config
	port        int
	server      *http.Server
	buffer      *CircularScreenshotBuffer
	captureCtx  context.Context
	captureStop context.CancelFunc
	mu          sync.RWMutex
	sessionID   string
	imageSvc    agentdomain.ImageService
	running     bool
	capture     func() error
	held        atomic.Bool
	wake        chan struct{}
	owedMu      sync.Mutex
	owed        chan struct{}
}

// NewScreenshotServer creates a new screenshot server
func NewScreenshotServer(cfg *config.Config, imageService agentdomain.ImageService, sessionID string) *ScreenshotServer {
	s := &ScreenshotServer{
		cfg:       cfg,
		sessionID: sessionID,
		imageSvc:  imageService,
		running:   false,
		wake:      make(chan struct{}, 1),
	}
	s.capture = s.captureScreenshot
	return s
}

// HoldChanged switches the capture pace as a person takes the screen or hands
// it back, without waiting out the interval in force.
func (s *ScreenshotServer) HoldChanged(held bool) {
	s.held.Store(held)
	s.captureSoon()
}

// InputLanded owes readers a frame captured after the input, and captures it.
func (s *ScreenshotServer) InputLanded() {
	s.owedMu.Lock()
	s.owed = make(chan struct{})
	s.owedMu.Unlock()
	s.captureSoon()
}

// captureSoon wakes the capture loop. A wake-up already waiting covers this one.
func (s *ScreenshotServer) captureSoon() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *ScreenshotServer) captureInterval() time.Duration {
	if s.held.Load() {
		return heldCaptureInterval
	}
	if seconds := s.cfg.ComputerUse.Screenshot.CaptureInterval; seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return defaultCaptureInterval
}

// captureOwedFrame captures once and settles the frame owed when it started.
// An input that lands during the capture stays owed for the next one.
func (s *ScreenshotServer) captureOwedFrame() error {
	s.owedMu.Lock()
	settling := s.owed
	s.owedMu.Unlock()

	err := s.capture()
	if settling == nil {
		return err
	}
	s.owedMu.Lock()
	if s.owed == settling {
		s.owed = nil
	}
	s.owedMu.Unlock()
	close(settling)
	return err
}

// awaitOwedFrame waits for the frame an input owes, so a reader right after
// an input sees its effect. It gives up after freshFrameWait.
func (s *ScreenshotServer) awaitOwedFrame() {
	s.owedMu.Lock()
	owed := s.owed
	s.owedMu.Unlock()
	if owed == nil {
		return
	}
	select {
	case <-owed:
	case <-time.After(freshFrameWait):
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
		var err error
		if tempDir, err = config.MediaDir("screenshots"); err != nil {
			return fmt.Errorf("failed to resolve temp directory path: %w", err)
		}
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
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
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

// Stop stops the HTTP server and capture loop. It releases the lock before
// Shutdown, which waits for in-flight handlers that take the read lock.
func (s *ScreenshotServer) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	server, buffer, captureStop := s.server, s.buffer, s.captureStop
	s.mu.Unlock()

	if captureStop != nil {
		captureStop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var shutdownErr error
	if server != nil {
		shutdownErr = server.Shutdown(ctx)
	}

	if buffer != nil {
		if err := buffer.Cleanup(); err != nil {
			logger.Warn("failed to cleanup buffer", "error", err)
		}
	}

	if shutdownErr != nil {
		return fmt.Errorf("failed to shutdown server: %w", shutdownErr)
	}
	return nil
}

// Port returns the port the server is listening on
func (s *ScreenshotServer) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.port
}

// startCaptureLoop captures at the interval in force, and at once when woken.
func (s *ScreenshotServer) startCaptureLoop() {
	timer := time.NewTimer(s.captureInterval())
	defer timer.Stop()

	failing := logCaptureResult(s.captureOwedFrame(), false)
	for {
		select {
		case <-s.captureCtx.Done():
			return
		case <-timer.C:
		case <-s.wake:
		}
		failing = logCaptureResult(s.captureOwedFrame(), failing)
		timer.Reset(s.captureInterval())
	}
}

// logCaptureResult warns once when capture starts failing and logs once when it
// recovers, so a screen that stays locked or unpermitted for hours doesn't flood
// the log with one warning per tick. It reports whether capture is now failing.
func logCaptureResult(err error, failing bool) bool {
	switch {
	case err != nil && !failing:
		logger.Warn("screenshot capture failed, suppressing repeats until it recovers", "error", err)
	case err != nil:
		logger.Debug("screenshot capture still failing", "error", err)
	case failing:
		logger.Info("screenshot capture recovered")
	}
	return err != nil
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
		Timestamp: time.Now(),
		Data:      imageAttachment.Data,
		Width:     width,
		Height:    height,
		Format:    s.cfg.ComputerUse.Screenshot.Format,
		Method:    displayProvider.GetDisplayInfo().Name,
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

// GetLatestFrame retrieves the latest screenshot from the buffer, after the
// frame a person's input owes when one is on its way.
func (s *ScreenshotServer) GetLatestFrame() (*agentdomain.Frame, error) {
	s.awaitOwedFrame()
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.buffer == nil {
		return nil, fmt.Errorf("screenshot buffer not initialized")
	}

	return s.buffer.GetLatest()
}
