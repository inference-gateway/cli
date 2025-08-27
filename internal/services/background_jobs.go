package services

import (
	"context"
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/logger"
)

// TitleGenerator interface for conversation title generation
type TitleGenerator interface {
	ProcessPendingTitles(ctx context.Context) error
}

// BackgroundJobManager manages background tasks
type BackgroundJobManager struct {
	titleGenerator TitleGenerator
	running        bool
	stopChan       chan struct{}
	wg             sync.WaitGroup
	mutex          sync.RWMutex
}

// NewBackgroundJobManager creates a new background job manager
func NewBackgroundJobManager(titleGenerator TitleGenerator) *BackgroundJobManager {
	return &BackgroundJobManager{
		titleGenerator: titleGenerator,
		stopChan:       make(chan struct{}),
	}
}

// Start begins running background jobs
func (m *BackgroundJobManager) Start(ctx context.Context) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.running {
		return
	}

	m.running = true
	logger.Info("Starting background job manager")

	m.wg.Add(1)
	go m.runTitleGenerationWorker(ctx)
}

// Stop stops all background jobs gracefully
func (m *BackgroundJobManager) Stop() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if !m.running {
		return
	}

	logger.Info("Stopping background job manager")
	close(m.stopChan)
	m.running = false

	done := make(chan struct{})
	go func() {
		defer close(done)
		m.wg.Wait()
	}()

	select {
	case <-done:
		logger.Info("Background job manager stopped gracefully")
	case <-time.After(10 * time.Second):
		logger.Warn("Background job manager stop timeout")
	}
}

// IsRunning returns whether the job manager is currently running
func (m *BackgroundJobManager) IsRunning() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.running
}

// runTitleGenerationWorker runs the conversation title generation job
func (m *BackgroundJobManager) runTitleGenerationWorker(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	logger.Debug("Title generation worker started")

	for {
		select {
		case <-ctx.Done():
			logger.Debug("Title generation worker stopped by context")
			return
		case <-m.stopChan:
			logger.Debug("Title generation worker stopped by signal")
			return
		case <-ticker.C:
			if m.titleGenerator != nil {
				if err := m.titleGenerator.ProcessPendingTitles(ctx); err != nil {
					logger.Error("Error processing pending titles", "error", err)
				}
			}
		}
	}
}

// TriggerTitleGeneration manually triggers title generation for pending conversations
func (m *BackgroundJobManager) TriggerTitleGeneration(ctx context.Context) error {
	if m.titleGenerator == nil {
		return nil
	}

	logger.Debug("Manually triggering title generation")
	return m.titleGenerator.ProcessPendingTitles(ctx)
}
