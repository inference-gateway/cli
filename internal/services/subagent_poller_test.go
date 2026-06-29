package services

import (
	"context"
	"sync"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// newFastInteractivePoller builds a poller wired for interactive watching with
// fast heuristic timings so tests don't wait on the real 2s/4s cadence.
func newFastInteractivePoller(tr domain.SubagentTracker, queue domain.MessageQueue, inspect PaneInspector) *SubagentPoller {
	p := NewSubagentPoller(tr, nil, queue, "req", nil)
	p.interactivePollInterval = 5 * time.Millisecond
	p.interactiveGrace = 0
	p.interactiveStableNeeded = 2
	p.SetPaneInspector(inspect)
	return p
}

// An interactive subagent whose pane goes idle and stable must notify the main
// agent (one enqueue) and be KEPT in the tracker marked completed (so its pane
// stays watchable / closeable), not re-notified.
func TestSubagentPoller_InteractiveCompletionNotifiesAndKeepsRecord(t *testing.T) {
	tr := utils.NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{
		ID: "i1", Label: "worker", Mode: domain.SubagentModeInteractive,
		PaneID: "%1", Status: domain.SubagentRunning,
	})
	queue := &domainmocks.FakeMessageQueue{}
	p := newFastInteractivePoller(tr, queue, func(_ context.Context, _ string) (string, bool, bool) {
		return "task done\nType your message", true, false // idle + stable
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitFor(t, func() bool { return queue.EnqueueCallCount() >= 1 }, "completion to be enqueued")

	s := tr.GetSubagent("i1")
	if s == nil {
		t.Fatalf("interactive subagent should be kept after completion, not removed")
	}
	if s.Status != domain.SubagentCompleted {
		t.Fatalf("interactive subagent should be marked completed, got %q", s.Status)
	}

	// A completed (non-running) subagent must not be re-watched / re-notified.
	time.Sleep(50 * time.Millisecond)
	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("expected exactly one completion notification, got %d", n)
	}
}

// A pane that disappears (user closed it) also notifies completion.
func TestSubagentPoller_InteractiveGonePaneNotifies(t *testing.T) {
	tr := utils.NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{
		ID: "i2", Label: "gone", Mode: domain.SubagentModeInteractive,
		PaneID: "%2", Status: domain.SubagentRunning,
	})
	queue := &domainmocks.FakeMessageQueue{}
	p := newFastInteractivePoller(tr, queue, func(_ context.Context, _ string) (string, bool, bool) {
		return "", false, true // gone
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitFor(t, func() bool { return queue.EnqueueCallCount() >= 1 }, "gone-pane completion to be enqueued")
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// When the poller stops (session ctx cancelled), any still-tracked subagent's
// CancelFunc must be called so detached (async) `infer agent` subprocesses are
// not left orphaned.
func TestSubagentPoller_StopCancelsDetachedSubagents(t *testing.T) {
	tr := utils.NewSubagentTracker()
	var mu sync.Mutex
	canceled := false
	_ = tr.AddSubagent(&domain.SubagentState{
		ID:         "x",
		Status:     domain.SubagentRunning,
		CancelFunc: func() { mu.Lock(); canceled = true; mu.Unlock() },
		ResultChan: make(chan *domain.ToolExecutionResult, 1),
		ErrorChan:  make(chan error, 1),
	})

	p := NewSubagentPoller(tr, nil, nil, "req", nil)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)
	cancel() // ctx.Done triggers stopAllMonitors

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := canceled
		mu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected detached subagent CancelFunc to be called on poller stop")
}
