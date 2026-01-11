package services

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	uuid "github.com/google/uuid"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// CircularScreenshotBuffer implements a thread-safe ring buffer for screenshots
// with optional disk persistence
type CircularScreenshotBuffer struct {
	screenshots  []*domain.Screenshot
	maxSize      int
	currentIndex int
	count        int
	mu           sync.RWMutex
	tempDir      string
	sessionID    string
}

// NewCircularScreenshotBuffer creates a new circular buffer for screenshots
func NewCircularScreenshotBuffer(maxSize int, tempDir string, sessionID string) (*CircularScreenshotBuffer, error) {
	sessionTempDir := filepath.Join(tempDir, fmt.Sprintf("session-%s", sessionID))
	if err := os.MkdirAll(sessionTempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &CircularScreenshotBuffer{
		screenshots:  make([]*domain.Screenshot, maxSize),
		maxSize:      maxSize,
		currentIndex: 0,
		count:        0,
		tempDir:      sessionTempDir,
		sessionID:    sessionID,
	}, nil
}

// Add adds a new screenshot to the buffer
// If the buffer is full, it evicts the oldest screenshot
func (b *CircularScreenshotBuffer) Add(screenshot *domain.Screenshot) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if screenshot.ID == "" {
		screenshot.ID = uuid.New().String()
	}

	if screenshot.Timestamp.IsZero() {
		screenshot.Timestamp = time.Now()
	}

	if b.count >= b.maxSize {
		oldScreenshot := b.screenshots[b.currentIndex]
		if oldScreenshot != nil {
			b.deleteFromDisk(oldScreenshot.ID)
		}
	}

	b.screenshots[b.currentIndex] = screenshot

	if err := b.writeToDisk(screenshot); err != nil {
		logger.Warn("Failed to write screenshot to disk", "error", err, "screenshot_id", screenshot.ID)
	}

	b.currentIndex = (b.currentIndex + 1) % b.maxSize
	if b.count < b.maxSize {
		b.count++
	}

	return nil
}

// GetLatest returns the most recent screenshot
func (b *CircularScreenshotBuffer) GetLatest() (*domain.Screenshot, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.count == 0 {
		return nil, fmt.Errorf("buffer is empty")
	}

	latestIndex := (b.currentIndex - 1 + b.maxSize) % b.maxSize
	return b.screenshots[latestIndex], nil
}

// GetByID returns a screenshot by its ID
func (b *CircularScreenshotBuffer) GetByID(id string) (*domain.Screenshot, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for i := 0; i < b.count; i++ {
		if b.screenshots[i] != nil && b.screenshots[i].ID == id {
			return b.screenshots[i], nil
		}
	}

	return nil, fmt.Errorf("screenshot not found: %s", id)
}

// GetRecent returns the N most recent screenshots
func (b *CircularScreenshotBuffer) GetRecent(limit int) []*domain.Screenshot {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit <= 0 || limit > b.count {
		limit = b.count
	}

	result := make([]*domain.Screenshot, 0, limit)

	for i := 0; i < limit; i++ {
		index := (b.currentIndex - 1 - i + b.maxSize) % b.maxSize
		if b.screenshots[index] != nil {
			result = append(result, b.screenshots[index])
		}
	}

	return result
}

// Count returns the current number of screenshots in the buffer
func (b *CircularScreenshotBuffer) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.count
}

// Clear removes all screenshots from the buffer and deletes disk files
func (b *CircularScreenshotBuffer) Clear() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i := 0; i < b.count; i++ {
		if b.screenshots[i] != nil {
			b.deleteFromDisk(b.screenshots[i].ID)
		}
	}

	b.screenshots = make([]*domain.Screenshot, b.maxSize)
	b.currentIndex = 0
	b.count = 0

	return nil
}

// Cleanup removes the temp directory and all screenshots
func (b *CircularScreenshotBuffer) Cleanup() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := os.RemoveAll(b.tempDir); err != nil {
		return fmt.Errorf("failed to cleanup temp directory: %w", err)
	}

	return nil
}

// writeToDisk writes a screenshot to disk as a PNG file
func (b *CircularScreenshotBuffer) writeToDisk(screenshot *domain.Screenshot) error {
	if screenshot.Data == "" {
		return nil
	}

	imageData, err := base64.StdEncoding.DecodeString(screenshot.Data)
	if err != nil {
		return fmt.Errorf("failed to decode base64 data: %w", err)
	}

	extension := screenshot.Format
	if extension == "" {
		extension = "png"
	}
	filename := filepath.Join(b.tempDir, fmt.Sprintf("screenshot-%s.%s", screenshot.ID, extension))
	if err := os.WriteFile(filename, imageData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// deleteFromDisk removes a screenshot file from disk
func (b *CircularScreenshotBuffer) deleteFromDisk(id string) {
	for _, ext := range []string{"png", "jpeg", "jpg"} {
		filename := filepath.Join(b.tempDir, fmt.Sprintf("screenshot-%s.%s", id, ext))
		if err := os.Remove(filename); err == nil {
			return
		}
	}
}
