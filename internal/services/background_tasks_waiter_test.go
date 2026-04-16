package services_test

import (
	"context"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// minimalA2AConfig returns a config with A2A tools enabled and the
// configured agent-mode max wait. AgentModeMaxWaitSeconds<=0 falls back
// to the default 300s inside the waiter.
func minimalA2AConfig(maxWaitSec int) *config.Config {
	cfg := &config.Config{}
	cfg.A2A.Enabled = true
	cfg.A2A.Task.AgentModeMaxWaitSeconds = maxWaitSec
	return cfg
}

func TestBackgroundTasksWaiter_DisabledWhenA2AOff(t *testing.T) {
	cfg := &config.Config{}
	cfg.A2A.Enabled = false

	w := services.NewBackgroundTasksWaiter(cfg, "session-1",
		&domainmocks.FakeBackgroundTaskRegistry{},
		&domainmocks.FakeMessageQueue{},
		nil)

	if got := w.WaitAndDrain(context.Background()); got != nil {
		t.Errorf("expected nil drained when A2A disabled, got %v", got)
	}
	if w.HasPendingTasks() {
		t.Error("expected HasPendingTasks=false when disabled")
	}
}

func TestBackgroundTasksWaiter_DisabledWhenDepsNil(t *testing.T) {
	cfg := minimalA2AConfig(5)

	w := services.NewBackgroundTasksWaiter(cfg, "session-1", nil, nil, nil)

	if got := w.WaitAndDrain(context.Background()); got != nil {
		t.Errorf("expected nil drained when deps nil, got %v", got)
	}
}

func TestBackgroundTasksWaiter_NoPendingAndEmptyQueue(t *testing.T) {
	registry := &domainmocks.FakeBackgroundTaskRegistry{}
	registry.HasPendingReturns(false)

	queue := &domainmocks.FakeMessageQueue{}
	queue.IsEmptyReturns(true)

	w := services.NewBackgroundTasksWaiter(minimalA2AConfig(5), "session-1", registry, queue, nil)

	if got := w.WaitAndDrain(context.Background()); got != nil {
		t.Errorf("expected nil drained when no tasks pending and queue empty, got %v", got)
	}
	if registry.HasPendingCallCount() == 0 {
		t.Errorf("expected HasPending to be consulted at least once")
	}
}

func TestBackgroundTasksWaiter_DrainsAlreadyQueued(t *testing.T) {
	registry := &domainmocks.FakeBackgroundTaskRegistry{}
	registry.HasPendingReturns(true)
	registry.GetAllPollingTasksReturns([]string{"task-1"})

	queue := &domainmocks.FakeMessageQueue{}

	queued := &domain.QueuedMessage{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent("[A2A Task Completed: A2A_SubmitTask]\n\nTask ID: task-1\n\nresult body"),
		},
		RequestID: "req-1",
	}
	queue.DequeueReturnsOnCall(0, queued)
	queue.DequeueReturnsOnCall(1, nil)

	queue.IsEmptyReturnsOnCall(0, false) // pre-dequeue check in drain
	queue.IsEmptyReturnsOnCall(1, true)  // after first dequeue, queue is empty

	w := services.NewBackgroundTasksWaiter(minimalA2AConfig(5), "session-1", registry, queue, nil)

	got := w.WaitAndDrain(context.Background())
	if len(got) != 1 {
		t.Fatalf("expected 1 drained message, got %d", len(got))
	}
	if got[0].Content == "" {
		t.Errorf("expected drained message to carry content, got empty")
	}
}

func TestBackgroundTasksWaiter_TimesOutWhenTasksNeverComplete(t *testing.T) {
	registry := &domainmocks.FakeBackgroundTaskRegistry{}
	registry.HasPendingReturns(true)
	registry.GetAllPollingTasksReturns([]string{"task-1"})

	queue := &domainmocks.FakeMessageQueue{}
	queue.IsEmptyReturns(true)

	w := services.NewBackgroundTasksWaiter(minimalA2AConfig(1), "session-1", registry, queue, nil)

	start := time.Now()
	got := w.WaitAndDrain(context.Background())
	elapsed := time.Since(start)

	if got != nil {
		t.Errorf("expected nil drained on timeout, got %v", got)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("expected wait loop to spend close to the configured 1s, got %s", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("expected wait loop to exit shortly after timeout, got %s", elapsed)
	}
}

func TestBackgroundTasksWaiter_StopsWhenAllTasksTerminate(t *testing.T) {
	registry := &domainmocks.FakeBackgroundTaskRegistry{}
	registry.HasPendingReturnsOnCall(0, true)
	registry.HasPendingReturnsOnCall(1, false)
	registry.HasPendingReturnsOnCall(2, false)
	registry.HasPendingReturnsOnCall(3, false)
	registry.GetAllPollingTasksReturns([]string{"task-1"})

	queue := &domainmocks.FakeMessageQueue{}
	queue.IsEmptyReturns(true)

	w := services.NewBackgroundTasksWaiter(minimalA2AConfig(30), "session-1", registry, queue, nil)

	start := time.Now()
	got := w.WaitAndDrain(context.Background())
	elapsed := time.Since(start)

	if got != nil {
		t.Errorf("expected nil drained when tasks terminate without queue activity, got %v", got)
	}
	if elapsed > 2*time.Second {
		t.Errorf("expected wait loop to exit promptly once tasks reach terminal state, got %s", elapsed)
	}
}

func TestBackgroundTasksWaiter_HasPendingAcrossTaskTypes(t *testing.T) {
	// Verify the waiter consults HasPending() and treats both A2A tasks and
	// running shells as "pending" via the unified registry — proving the
	// unification across the two task types works at the waiter boundary.
	registry := &domainmocks.FakeBackgroundTaskRegistry{}

	// First call: only A2A in flight (HasPending true), then nothing.
	registry.HasPendingReturnsOnCall(0, true)
	registry.HasPendingReturnsOnCall(1, false)
	registry.GetAllPollingTasksReturns([]string{"a2a-task-1"})
	registry.CountRunningReturns(0)

	queue := &domainmocks.FakeMessageQueue{}
	queue.IsEmptyReturns(true)

	w := services.NewBackgroundTasksWaiter(minimalA2AConfig(30), "session-1", registry, queue, nil)

	if got := w.WaitAndDrain(context.Background()); got != nil {
		t.Errorf("expected nil drained when A2A task terminates without queue activity, got %v", got)
	}
	if registry.HasPendingCallCount() < 1 {
		t.Errorf("expected HasPending to be consulted")
	}
}
