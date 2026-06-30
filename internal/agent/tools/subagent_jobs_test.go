package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// fastInteractiveJob builds an interactive subagent job with a fake pane inspector
// and fast heuristic tunables for deterministic, quick tests.
func fastInteractiveJob(inspect func() domain.PaneObservation) *interactiveSubagentJob {
	return &interactiveSubagentJob{
		state:        &domain.SubagentState{ID: "s1", Label: "sub", SessionID: "sess", PaneID: "pane", StartedAt: time.Now()},
		inspect:      func(_ context.Context, _, _ string) domain.PaneObservation { return inspect() },
		pollInterval: 5 * time.Millisecond,
		grace:        0,
		stableNeeded: 2,
	}
}

func runJobCollecting(j *interactiveSubagentJob) (stop func(), notes func() []string) {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var got []string
	go j.Run(ctx, func(sig domain.JobSignal) {
		mu.Lock()
		got = append(got, sig.Note)
		mu.Unlock()
	})
	return cancel, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

func waitUntil(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func TestInteractiveSubagentJob_HarvestEmitsCompletionOnce(t *testing.T) {
	j := fastInteractiveJob(func() domain.PaneObservation {
		return domain.PaneObservation{Harvested: "the subagent's real answer"}
	})
	stop, notes := runJobCollecting(j)
	defer stop()

	waitUntil(t, func() bool { return len(notes()) >= 1 }, "the harvested turn to be emitted")
	// Give it a few more ticks; the same harvest must not re-emit.
	time.Sleep(30 * time.Millisecond)

	all := notes()
	if len(all) != 1 {
		t.Fatalf("emitted %d notes, want 1: %v", len(all), all)
	}
	if !strings.Contains(all[0], "the subagent's real answer") || !strings.Contains(all[0], "Subagent Completed") {
		t.Fatalf("completion note missing harvested answer: %q", all[0])
	}
}

func TestInteractiveSubagentJob_IdleFallbackEmits(t *testing.T) {
	j := fastInteractiveJob(func() domain.PaneObservation {
		return domain.PaneObservation{Screen: "frozen idle prompt"} // stable, no result file
	})
	stop, notes := runJobCollecting(j)
	defer stop()

	waitUntil(t, func() bool { return len(notes()) >= 1 }, "the idle-fallback completion to be emitted")
	if all := notes(); !strings.Contains(all[0], "No final message was captured") {
		t.Fatalf("idle note wrong: %q", all[0])
	}
}

// TestInteractiveSubagentJob_BusyPaneNotFalselyCompleted is the regression guard:
// a working subagent whose elapsed-time spinner changes every poll must NOT be
// reported complete.
func TestInteractiveSubagentJob_BusyPaneNotFalselyCompleted(t *testing.T) {
	var n int
	var mu sync.Mutex
	j := fastInteractiveJob(func() domain.PaneObservation {
		mu.Lock()
		n++
		s := fmt.Sprintf("Thinking... (%d.0s)", n)
		mu.Unlock()
		return domain.PaneObservation{Screen: s}
	})
	stop, notes := runJobCollecting(j)
	defer stop()

	time.Sleep(60 * time.Millisecond) // many polls
	if all := notes(); len(all) != 0 {
		t.Fatalf("busy pane falsely completed: %v", all)
	}
}

func TestInteractiveSubagentJob_AwaitingApprovalEmittedOnce(t *testing.T) {
	j := fastInteractiveJob(func() domain.PaneObservation {
		return domain.PaneObservation{AwaitingApproval: true, ApprovalSummary: "Bash(rm -rf /tmp/x)"}
	})
	stop, notes := runJobCollecting(j)
	defer stop()

	waitUntil(t, func() bool { return len(notes()) >= 1 }, "approval to be announced")
	time.Sleep(30 * time.Millisecond)
	all := notes()
	if len(all) != 1 {
		t.Fatalf("approval announced %d times, want 1: %v", len(all), all)
	}
	if !strings.Contains(all[0], "Awaiting Approval") || !strings.Contains(all[0], "ApproveSubagent") {
		t.Fatalf("approval note wrong: %q", all[0])
	}
}

func TestInteractiveSubagentJob_PaneGoneReturns(t *testing.T) {
	j := fastInteractiveJob(func() domain.PaneObservation {
		return domain.PaneObservation{Gone: true}
	})
	done := make(chan domain.ToolExecutionResult, 1)
	go func() { done <- j.Run(context.Background(), func(domain.JobSignal) {}) }()

	select {
	case res := <-done:
		if !res.Success {
			t.Fatalf("expected success result when pane closes, got %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not return when the pane was gone")
	}
}
