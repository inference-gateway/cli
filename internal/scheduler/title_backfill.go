package scheduler

import (
	"context"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// TitleBackfill manages background tasks. cancel and done belong to the
// current run and are nil while stopped.
type TitleBackfill struct {
	titleGenerator scheddomain.TitleGenerator
	config         *config.Config
	mutex          sync.RWMutex
	cancel         context.CancelFunc
	done           chan struct{}
}

// NewTitleBackfill creates a new background job manager
func NewTitleBackfill(titleGenerator scheddomain.TitleGenerator, config *config.Config) *TitleBackfill {
	return &TitleBackfill{
		titleGenerator: titleGenerator,
		config:         config,
	}
}

// Start begins running background jobs
func (m *TitleBackfill) Start(ctx context.Context) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.cancel != nil {
		return
	}

	logger.Info("starting background job manager")
	ctx, m.cancel = context.WithCancel(ctx)
	m.done = make(chan struct{})
	go m.runTitleGenerationWorker(ctx, m.done)
}

// Stop cancels the worker, including a title pass in flight, and waits for it
// outside the lock.
func (m *TitleBackfill) Stop() {
	m.mutex.Lock()
	cancel, done := m.cancel, m.done
	m.cancel, m.done = nil, nil
	m.mutex.Unlock()

	if cancel == nil {
		return
	}

	logger.Info("stopping background job manager")
	cancel()

	select {
	case <-done:
		logger.Info("background job manager stopped gracefully")
	case <-time.After(10 * time.Second):
		logger.Warn("background job manager stop timeout")
	}
}

// IsRunning returns whether the job manager is currently running
func (m *TitleBackfill) IsRunning() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.cancel != nil
}

// runTitleGenerationWorker runs the conversation title generation job
func (m *TitleBackfill) runTitleGenerationWorker(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	interval := m.getJobInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.titleGenerator != nil {
				if err := m.titleGenerator.ProcessPendingTitles(ctx); err != nil {
					logger.Error("error processing pending titles", "error", err)
				}
			}
		}
	}
}

// TriggerTitleGeneration manually triggers title generation for pending conversations
func (m *TitleBackfill) TriggerTitleGeneration(ctx context.Context) error {
	if m.titleGenerator == nil {
		return nil
	}

	return m.titleGenerator.ProcessPendingTitles(ctx)
}

// getJobInterval returns the configured interval for background jobs
func (m *TitleBackfill) getJobInterval() time.Duration {
	if m.config != nil && m.config.Conversation.TitleGeneration.Interval > 0 {
		return time.Duration(m.config.Conversation.TitleGeneration.Interval) * time.Second
	}
	return 5 * time.Minute
}
