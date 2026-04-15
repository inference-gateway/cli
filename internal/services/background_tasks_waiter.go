package services

import (
	"context"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// DrainedMessage is the opaque payload returned by BackgroundTasksWaiter.WaitAndDrain.
// Anything that lands on the shared message queue (A2A task completions,
// background shell completions, user messages typed while the agent is busy)
// is surfaced through this same shape.
type DrainedMessage struct {
	Content   string
	Timestamp time.Time
}

// BackgroundTasksWaiter encapsulates the "wait for in-flight background work
// to quiesce, then drain the shared message queue" pattern that batch
// (`infer agent`) mode needs between turns.
//
// The notification mechanism is the queue itself: when background work
// completes (A2A task, bash shell, etc.), it pushes a result onto the shared
// MessageQueueService. This waiter polls at 1s intervals until the queue has
// something to drain or all background work finishes.
//
// This is the batch-mode counterpart to chat mode's CheckingQueueState.
type BackgroundTasksWaiter struct {
	cfg          *config.Config
	registry     domain.BackgroundTaskRegistry
	messageQueue domain.MessageQueue
	poller       *A2ATaskPoller
	enabled      bool
}

// NewBackgroundTasksWaiter constructs a waiter. If A2A tools are disabled or
// either of the underlying services is nil, the returned waiter is a no-op
// for every method, so callers can use it unconditionally.
func NewBackgroundTasksWaiter(
	cfg *config.Config,
	sessionID string,
	registry domain.BackgroundTaskRegistry,
	messageQueue domain.MessageQueue,
	conversationRepo domain.ConversationRepository,
) *BackgroundTasksWaiter {
	w := &BackgroundTasksWaiter{
		cfg:          cfg,
		registry:     registry,
		messageQueue: messageQueue,
	}

	if !cfg.IsA2AToolsEnabled() || registry == nil || messageQueue == nil {
		return w
	}

	w.enabled = true
	w.poller = NewA2ATaskPoller(
		registry,
		nil,
		messageQueue,
		sessionID,
		conversationRepo,
	)

	return w
}

// Start launches the A2A polling goroutine. No-op if A2A is disabled.
func (w *BackgroundTasksWaiter) Start(ctx context.Context) {
	if !w.enabled {
		return
	}
	go w.poller.Start(ctx)
}

// Stop terminates the A2A polling goroutine. No-op if A2A is disabled.
func (w *BackgroundTasksWaiter) Stop() {
	if !w.enabled {
		return
	}
	w.poller.Stop()
}

// HasPendingTasks reports whether any background work is still in flight.
func (w *BackgroundTasksWaiter) HasPendingTasks() bool {
	if !w.enabled {
		return false
	}
	return w.registry.HasPending()
}

// WaitAndDrain polls until background tasks complete and the queue has
// results, or the per-session timeout fires. Returns drained payloads.
// Returns nil if disabled or no work was pending.
func (w *BackgroundTasksWaiter) WaitAndDrain(ctx context.Context) []DrainedMessage {
	if !w.enabled {
		return nil
	}

	if drained := w.drainQueue(); len(drained) > 0 {
		return drained
	}

	if !w.HasPendingTasks() {
		return nil
	}

	maxWaitSec := w.cfg.A2A.Task.AgentModeMaxWaitSeconds
	if maxWaitSec <= 0 {
		maxWaitSec = 300
	}
	deadline := time.NewTimer(time.Duration(maxWaitSec) * time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	logger.Info("Waiting for pending background tasks to complete",
		"pending_a2a_tasks", len(w.registry.GetAllPollingTasks()),
		"running_shells", w.registry.CountRunning(),
		"max_wait_seconds", maxWaitSec)

	for {
		select {
		case <-ticker.C:
		case <-deadline.C:
			logger.Warn("Timed out waiting for background tasks to complete",
				"pending_a2a_tasks", len(w.registry.GetAllPollingTasks()),
				"running_shells", w.registry.CountRunning(),
				"max_wait_seconds", maxWaitSec)
			return w.drainQueue()
		case <-ctx.Done():
			return w.drainQueue()
		}

		if drained := w.drainQueue(); len(drained) > 0 {
			return drained
		}
		if !w.HasPendingTasks() {
			return nil
		}
	}
}

// drainQueue pulls every queued message off the shared message queue.
func (w *BackgroundTasksWaiter) drainQueue() []DrainedMessage {
	var drained []DrainedMessage
	for !w.messageQueue.IsEmpty() {
		queued := w.messageQueue.Dequeue()
		if queued == nil {
			break
		}

		content, err := queued.Message.Content.AsMessageContent0()
		if err != nil || content == "" {
			continue
		}

		drained = append(drained, DrainedMessage{
			Content:   content,
			Timestamp: time.Now(),
		})
	}

	if len(drained) > 0 {
		logger.Info("Drained background task results from queue", "count", len(drained))
	}
	return drained
}
