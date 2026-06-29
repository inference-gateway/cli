package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
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

// When the subagent's chat writes its real last message to its result file
// (the inspector returns it as `harvested`), the poller delivers it immediately,
// keeps the record marked completed (pane stays watchable), and does not re-notify.
func TestSubagentPoller_InteractiveHarvestNotifiesAndKeepsRecord(t *testing.T) {
	tr := utils.NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{
		ID: "i1", Label: "worker", Mode: domain.SubagentModeInteractive,
		PaneID: "%1", SessionID: "sess-1", Status: domain.SubagentRunning,
	})
	queue := &domainmocks.FakeMessageQueue{}
	p := newFastInteractivePoller(tr, queue, func(_ context.Context, _, _ string) domain.PaneObservation {
		return domain.PaneObservation{Harvested: "the subagent's real answer"}
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

	time.Sleep(50 * time.Millisecond)
	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("expected exactly one completion notification, got %d", n)
	}
}

// With no result file yet, the poller falls back to idle-by-stability: a
// genuinely idle pane returns the SAME screen snapshot every poll (no elapsed-
// time spinner to change it), and once that is stable the subagent is completed.
func TestSubagentPoller_InteractiveIdleFallbackNotifies(t *testing.T) {
	tr := utils.NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{
		ID: "i3", Label: "idle", Mode: domain.SubagentModeInteractive,
		PaneID: "%3", SessionID: "sess-3", Status: domain.SubagentRunning,
	})
	queue := &domainmocks.FakeMessageQueue{}
	p := newFastInteractivePoller(tr, queue, func(_ context.Context, _, _ string) domain.PaneObservation {
		return domain.PaneObservation{Screen: "idle pane - waiting for input"}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitFor(t, func() bool { return queue.EnqueueCallCount() >= 1 }, "idle-fallback completion to be enqueued")
}

// REGRESSION (the reported bug): a working subagent's pane changes every poll
// (the chat's elapsed-time spinner), so it must NEVER be mistaken for a finished
// turn while still exploring. Once it actually finishes and its result file
// appears (Harvested), the real answer is delivered exactly once.
func TestSubagentPoller_BusyPaneIsNotFalselyCompleted(t *testing.T) {
	tr := utils.NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{
		ID: "b1", Label: "busy", Mode: domain.SubagentModeInteractive,
		PaneID: "%1", SessionID: "sess-b", Status: domain.SubagentRunning,
	})
	queue := &domainmocks.FakeMessageQueue{}
	var mu sync.Mutex
	tick := 0
	finished := false
	p := newFastInteractivePoller(tr, queue, func(_ context.Context, _, _ string) domain.PaneObservation {
		mu.Lock()
		defer mu.Unlock()
		if finished {
			return domain.PaneObservation{Harvested: "the real answer"}
		}
		tick++
		return domain.PaneObservation{Screen: fmt.Sprintf("Thinking... (%d.0s)", tick)}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	time.Sleep(120 * time.Millisecond)
	if n := queue.EnqueueCallCount(); n != 0 {
		t.Fatalf("a busy (changing) pane must not be completed; got %d notification(s)", n)
	}

	mu.Lock()
	finished = true
	mu.Unlock()

	waitFor(t, func() bool { return queue.EnqueueCallCount() >= 1 }, "harvest completion after work finishes")
	msg, _ := queue.EnqueueArgsForCall(0)
	text, _ := msg.Content.AsMessageContent0()
	if !strings.Contains(text, "the real answer") {
		t.Fatalf("expected the harvested answer to be delivered; got %q", text)
	}
}

// A pane that disappears (user closed it) also notifies completion.
func TestSubagentPoller_InteractiveGonePaneNotifies(t *testing.T) {
	tr := utils.NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{
		ID: "i2", Label: "gone", Mode: domain.SubagentModeInteractive,
		PaneID: "%2", SessionID: "sess-2", Status: domain.SubagentRunning,
	})
	queue := &domainmocks.FakeMessageQueue{}
	p := newFastInteractivePoller(tr, queue, func(_ context.Context, _, _ string) domain.PaneObservation {
		return domain.PaneObservation{Gone: true}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitFor(t, func() bool { return queue.EnqueueCallCount() >= 1 }, "gone-pane completion to be enqueued")
}

// A pane whose process exited (Dead, kept open by remain-on-exit) - distinct from
// Gone - also notifies completion.
func TestSubagentPoller_InteractiveDeadPaneNotifies(t *testing.T) {
	tr := utils.NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{
		ID: "i4", Label: "dead", Mode: domain.SubagentModeInteractive,
		PaneID: "%4", SessionID: "sess-4", Status: domain.SubagentRunning,
	})
	queue := &domainmocks.FakeMessageQueue{}
	p := newFastInteractivePoller(tr, queue, func(_ context.Context, _, _ string) domain.PaneObservation {
		return domain.PaneObservation{Dead: true}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitFor(t, func() bool { return queue.EnqueueCallCount() >= 1 }, "dead-pane completion to be enqueued")
}

// An interactive subagent blocked on a tool-approval prompt is announced once
// (not every poll), stays running (the poller does NOT finish it), and the
// message tells the agent to relay with ApproveSubagent.
func TestSubagentPoller_AwaitingApprovalNotifiesOnce(t *testing.T) {
	tr := utils.NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{
		ID: "a1", Label: "worker", Mode: domain.SubagentModeInteractive,
		PaneID: "%1", SessionID: "sess-a", Status: domain.SubagentRunning,
	})
	queue := &domainmocks.FakeMessageQueue{}
	p := newFastInteractivePoller(tr, queue, func(_ context.Context, _, _ string) domain.PaneObservation {
		return domain.PaneObservation{AwaitingApproval: true, ApprovalSummary: "Bash rm -rf build"}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitFor(t, func() bool { return queue.EnqueueCallCount() >= 1 }, "approval notification to be enqueued")

	time.Sleep(60 * time.Millisecond)
	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("expected exactly one approval notification, got %d", n)
	}
	if s := tr.GetSubagent("a1"); s == nil || s.Status != domain.SubagentRunning {
		t.Fatalf("a subagent awaiting approval must stay running, got %v", s)
	}
	msg, _ := queue.EnqueueArgsForCall(0)
	text, _ := msg.Content.AsMessageContent0()
	if !strings.Contains(text, "Awaiting Approval") || !strings.Contains(text, "ApproveSubagent") {
		t.Fatalf("approval notification should name the action; got %q", text)
	}
}

// After a subagent completes, flipping its status back to running (as
// SendSubagentInput does on re-prompt) must cause the poller to re-watch it and
// deliver a second completion - so re-prompting still notifies, no polling.
func TestSubagentPoller_ReArmReWatchesAfterStatusRunning(t *testing.T) {
	tr := utils.NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{
		ID: "r1", Label: "worker", Mode: domain.SubagentModeInteractive,
		PaneID: "%1", SessionID: "sess-r", Status: domain.SubagentRunning,
	})
	queue := &domainmocks.FakeMessageQueue{}
	var mu sync.Mutex
	harvest := "first answer"
	p := newFastInteractivePoller(tr, queue, func(_ context.Context, _, _ string) domain.PaneObservation {
		mu.Lock()
		defer mu.Unlock()
		return domain.PaneObservation{Harvested: harvest}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Start(ctx)

	waitFor(t, func() bool { return queue.EnqueueCallCount() >= 1 }, "first completion")

	mu.Lock()
	harvest = "second answer"
	mu.Unlock()
	_ = tr.SetSubagentStatus("r1", domain.SubagentRunning)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if queue.EnqueueCallCount() >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected a second completion after re-arm, got %d", queue.EnqueueCallCount())
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
	cancel()

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
