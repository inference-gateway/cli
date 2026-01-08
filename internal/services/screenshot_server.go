package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"

	_ "github.com/inference-gateway/cli/internal/display/wayland"
	_ "github.com/inference-gateway/cli/internal/display/x11"
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
			logger.Error("Screenshot server error", "error", err)
		}
	}()

	s.captureCtx, s.captureStop = context.WithCancel(context.Background())
	go s.startCaptureLoop()

	s.running = true

	// Note: Border overlay is now managed by FloatingWindow.app via BorderOverlayEvent

	interval := s.cfg.ComputerUse.Screenshot.CaptureInterval
	if interval <= 0 {
		interval = 3
	}
	logger.Info("Screenshot server started",
		"session_id", s.sessionID,
		"port", s.port,
		"buffer_size", bufferSize,
		"temp_dir", absTempDir,
		"capture_interval", interval)

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
		interval = 3
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.captureCtx.Done():
			return
		case <-ticker.C:
			if err := s.captureScreenshot(); err != nil {
				logger.Warn("Screenshot capture failed", "error", err)
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
			logger.Warn("Failed to close controller", "error", closeErr)
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
		logger.Warn("Failed to decode image config, using controller dimensions", "error", err)
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
		logger.Warn("Failed to get logical dimensions", "error", err)
	} else if width != logicalWidth || height != logicalHeight {
		img = resizeImage(img, logicalWidth, logicalHeight)
		width = logicalWidth
		height = logicalHeight
	}

	originalWidth := width
	originalHeight := height

	targetW := s.cfg.ComputerUse.Screenshot.TargetWidth
	targetH := s.cfg.ComputerUse.Screenshot.TargetHeight

	if targetW > 0 && targetH > 0 {
		img = resizeImage(img, targetW, targetH)
		width = targetW
		height = targetH
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

	screenshot := &domain.Screenshot{
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
		logger.Warn("Failed to encode screenshot response", "error", err)
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

	status := map[string]any{
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
func (s *ScreenshotServer) GetLatestScreenshot() (*domain.Screenshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.buffer == nil {
		return nil, fmt.Errorf("screenshot buffer not initialized")
	}

	return s.buffer.GetLatest()
}

// resizeImage resizes an image to target dimensions using bilinear interpolation
// This provides better quality than nearest neighbor for LLM visual understanding
func resizeImage(src image.Image, targetWidth, targetHeight int) image.Image {
	srcBounds := src.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))

	xRatio := float64(srcWidth-1) / float64(targetWidth-1)
	yRatio := float64(srcHeight-1) / float64(targetHeight-1)

	for dstY := range targetHeight {
		for dstX := range targetWidth {
			srcXFloat := float64(dstX) * xRatio
			srcYFloat := float64(dstY) * yRatio

			srcX := int(srcXFloat)
			srcY := int(srcYFloat)

			fracX := srcXFloat - float64(srcX)
			fracY := srcYFloat - float64(srcY)

			srcX1 := srcX
			srcY1 := srcY
			srcX2 := srcX + 1
			srcY2 := srcY + 1

			if srcX2 >= srcWidth {
				srcX2 = srcWidth - 1
			}
			if srcY2 >= srcHeight {
				srcY2 = srcHeight - 1
			}

			c11 := src.At(srcBounds.Min.X+srcX1, srcBounds.Min.Y+srcY1)
			c21 := src.At(srcBounds.Min.X+srcX2, srcBounds.Min.Y+srcY1)
			c12 := src.At(srcBounds.Min.X+srcX1, srcBounds.Min.Y+srcY2)
			c22 := src.At(srcBounds.Min.X+srcX2, srcBounds.Min.Y+srcY2)

			r11, g11, b11, a11 := c11.RGBA()
			r21, g21, b21, a21 := c21.RGBA()
			r12, g12, b12, a12 := c12.RGBA()
			r22, g22, b22, a22 := c22.RGBA()

			w1 := (1 - fracX) * (1 - fracY)
			w2 := fracX * (1 - fracY)
			w3 := (1 - fracX) * fracY
			w4 := fracX * fracY

			r := uint8((float64(r11)*w1 + float64(r21)*w2 + float64(r12)*w3 + float64(r22)*w4) / 257)
			g := uint8((float64(g11)*w1 + float64(g21)*w2 + float64(g12)*w3 + float64(g22)*w4) / 257)
			b := uint8((float64(b11)*w1 + float64(b21)*w2 + float64(b12)*w3 + float64(b22)*w4) / 257)
			a := uint8((float64(a11)*w1 + float64(a21)*w2 + float64(a12)*w3 + float64(a22)*w4) / 257)

			dst.SetRGBA(dstX, dstY, color.RGBA{R: r, G: g, B: b, A: a})
		}
	}

	return dst
}
