package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
)

// fastInteractiveJob builds an interactive subagent job with a fake pane inspector,
// a real tracker-backed tool and fast heuristic tunables. The pane id is blank so
// teardown never reaches the host's tmux. The idle timeout defaults to well past
// every test's lifetime and the timeout tests shorten it.
func fastInteractiveJob(inspect func() scheddomain.PaneObservation) *interactiveSubagentJob {
	return fastInteractiveJobWithTimeout(inspect, 2*time.Second)
}

func fastInteractiveJobWithTimeout(inspect func() scheddomain.PaneObservation, idleTimeout time.Duration) *interactiveSubagentJob {
	state := &scheddomain.SubagentState{ID: "s1", Label: "sub", SessionID: "sess", Status: scheddomain.SubagentRunning, StartedAt: time.Now()}
	tool := &AgentTool{tracker: schedinfra.NewSubagentTracker()}
	_ = tool.tracker.AddSubagent(state)
	return &interactiveSubagentJob{
		tool:         tool,
		state:        state,
		inspect:      func(_ context.Context, _, _ string) scheddomain.PaneObservation { return inspect() },
		pollInterval: 5 * time.Millisecond,
		grace:        0,
		stableNeeded: 2,
		idleTimeout:  idleTimeout,
	}
}

func runJobCollecting(j *interactiveSubagentJob, ctx context.Context) (result <-chan agentdomain.ToolExecutionResult, notes func() []string) {
	var mu sync.Mutex
	var got []string
	out := make(chan agentdomain.ToolExecutionResult, 1)
	go func() {
		out <- j.Run(ctx, func(sig scheddomain.JobSignal) {
			mu.Lock()
			got = append(got, sig.Note)
			mu.Unlock()
		})
	}()
	return out, func() []string {
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
	j := fastInteractiveJob(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{Harvested: "the subagent's real answer"}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, notes := runJobCollecting(j, ctx)

	waitUntil(t, func() bool { return len(notes()) >= 1 }, "the harvested turn to be emitted")
	time.Sleep(30 * time.Millisecond)

	all := notes()
	if len(all) != 1 {
		t.Fatalf("emitted %d notes, want 1: %v", len(all), all)
	}
	if !strings.Contains(all[0], "the subagent's real answer") || !strings.Contains(all[0], "Subagent Completed") {
		t.Fatalf("completion note missing harvested answer: %q", all[0])
	}
	if j.Output() != "the subagent's real answer" {
		t.Fatalf("Output() = %q, want the harvested turn for the /tasks detail panel", j.Output())
	}
	select {
	case <-result:
		t.Fatalf("a result-file write without done (older binary) must not close the subagent")
	default:
	}
}

func TestInteractiveSubagentJob_IdleFallbackEmits(t *testing.T) {
	j := fastInteractiveJob(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{Screen: "frozen idle prompt"}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, notes := runJobCollecting(j, ctx)

	waitUntil(t, func() bool { return len(notes()) >= 1 }, "the idle warning to be emitted")
	all := notes()
	if strings.Contains(all[0], "Subagent Completed") || !strings.Contains(all[0], "[Subagent Idle: sub]") {
		t.Fatalf("idle fallback must not masquerade as a completion: %q", all[0])
	}
	if !strings.Contains(all[0], "will be closed in") {
		t.Fatalf("idle note must name the pending auto-close: %q", all[0])
	}
}

// TestInteractiveSubagentJob_BusyPaneNotFalselyCompleted is the regression guard:
// a working subagent whose elapsed-time spinner changes every poll must NOT be
// reported complete or closed.
func TestInteractiveSubagentJob_BusyPaneNotFalselyCompleted(t *testing.T) {
	var n int
	var mu sync.Mutex
	j := fastInteractiveJob(func() scheddomain.PaneObservation {
		mu.Lock()
		n++
		s := fmt.Sprintf("Thinking... (%d.0s)", n)
		mu.Unlock()
		return scheddomain.PaneObservation{Screen: s}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, notes := runJobCollecting(j, ctx)

	time.Sleep(60 * time.Millisecond)
	if all := notes(); len(all) != 0 {
		t.Fatalf("busy pane falsely completed: %v", all)
	}
	select {
	case res := <-result:
		t.Fatalf("busy pane closed: %+v", res)
	default:
	}
}

// TestInteractiveSubagentJob_DoneTurnClosesSubagent pins the done signal: a
// result-file write with done set is exactly one completion note plus the inline
// teardown (temp files removed, tracker status completed) and a returned Run.
func TestInteractiveSubagentJob_DoneTurnClosesSubagent(t *testing.T) {
	t.Cleanup(func() { _ = os.Remove(subagentResultFilePath("sess")) })
	writeTestResultFile(t, "sess", "seeded so teardown's removal is observable")

	j := fastInteractiveJob(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{Harvested: "the subagent's real answer", Done: true}
	})
	result, notes := runJobCollecting(j, context.Background())

	select {
	case res := <-result:
		if !res.Success {
			t.Fatalf("expected a successful terminal turn, got %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("a done turn must close the subagent and return")
	}

	all := notes()
	if len(all) != 1 {
		t.Fatalf("emitted %d notes, want exactly one completion note: %v", len(all), all)
	}
	if !strings.Contains(all[0], "the subagent's real answer") || !strings.Contains(all[0], "[Subagent Completed: sub]") {
		t.Fatalf("done note missing the harvested answer: %q", all[0])
	}
	if _, err := os.Stat(subagentResultFilePath("sess")); !os.IsNotExist(err) {
		t.Fatalf("teardown must remove the result file")
	}
	if entry := j.tool.tracker.GetSubagent("s1"); entry == nil || entry.Status != scheddomain.SubagentCompleted {
		t.Fatalf("done teardown must record the completed status, got %+v", entry)
	}
}

// TestInteractiveSubagentJob_FailedTurnClosesWith covers the failed terminal turn:
// a result file with success=false counts as done too, closed with the error in
// the note and the tracker entry marked failed.
func TestInteractiveSubagentJob_FailedTurnClosesWith(t *testing.T) {
	t.Cleanup(func() { _ = os.Remove(subagentResultFilePath("sess")) })
	writeTestResultFile(t, "sess", "seeded so teardown's removal is observable")

	j := fastInteractiveJob(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{Harvested: "partial answer", Done: true, HarvestFailed: true, HarvestError: "boom"}
	})
	result, notes := runJobCollecting(j, context.Background())

	select {
	case res := <-result:
		if res.Success {
			t.Fatalf("a failed terminal turn must be reported failed, got %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("a failed terminal turn must close the subagent and return")
	}

	all := notes()
	if len(all) != 1 {
		t.Fatalf("emitted %d notes, want exactly one failed-turn note: %v", len(all), all)
	}
	if !strings.Contains(all[0], "Subagent Completed") || !strings.Contains(all[0], "Error: boom") {
		t.Fatalf("failed-turn note must carry the error: %q", all[0])
	}
	if entry := j.tool.tracker.GetSubagent("s1"); entry == nil || entry.Status != scheddomain.SubagentFailed {
		t.Fatalf("failed teardown must record the failed status, got %+v", entry)
	}
}

// TestInteractiveSubagentJob_FailedTurnWithoutAnswerCloses covers the commonest
// failure: the first request errors, so the done write carries no assistant text.
func TestInteractiveSubagentJob_FailedTurnWithoutAnswerCloses(t *testing.T) {
	j := fastInteractiveJob(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{Done: true, HarvestFailed: true, HarvestError: "boom"}
	})
	result, notes := runJobCollecting(j, context.Background())

	select {
	case res := <-result:
		if res.Success {
			t.Fatalf("a failed terminal turn must be reported failed, got %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("a failed turn without an answer must still close the subagent")
	}

	if all := notes(); len(all) != 1 || !strings.Contains(all[0], "Error: boom") {
		t.Fatalf("want exactly one note carrying the error, got %v", all)
	}
	if entry := j.tool.tracker.GetSubagent("s1"); entry == nil || entry.Status != scheddomain.SubagentFailed {
		t.Fatalf("failed teardown must record the failed status, got %+v", entry)
	}
}

// TestInteractiveSubagentJob_CancelTearsDown guards the WindStop path: a cancelled
// monitor removes the temp files itself because reap's Close no longer does.
func TestInteractiveSubagentJob_CancelTearsDown(t *testing.T) {
	t.Cleanup(func() { _ = os.Remove(subagentResultFilePath("sess")) })
	writeTestResultFile(t, "sess", "seeded so teardown's removal is observable")

	j := fastInteractiveJob(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{AwaitingApproval: true}
	})
	ctx, cancel := context.WithCancel(context.Background())
	result, _ := runJobCollecting(j, ctx)
	cancel()

	select {
	case <-result:
	case <-time.After(2 * time.Second):
		t.Fatalf("a cancelled monitor must return")
	}
	if _, err := os.Stat(subagentResultFilePath("sess")); !os.IsNotExist(err) {
		t.Fatalf("a cancelled monitor must remove the result file")
	}
}

// TestInteractiveSubagentJob_IdleTimeoutCloses pins the safety net: a pane that
// never reports done is closed after the configured inactivity timeout, emitting
// one close note instead of sitting open forever.
func TestInteractiveSubagentJob_IdleTimeoutCloses(t *testing.T) {
	j := fastInteractiveJobWithTimeout(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{Screen: "frozen idle prompt"}
	}, 200*time.Millisecond)
	result, notes := runJobCollecting(j, context.Background())

	select {
	case <-result:
	case <-time.After(2 * time.Second):
		t.Fatalf("the idle timeout must close the subagent and return")
	}

	all := notes()
	if len(all) != 2 {
		t.Fatalf("emitted %d notes, want the idle warning then the close note: %v", len(all), all)
	}
	if !strings.Contains(all[0], "[Subagent Idle: sub]") {
		t.Fatalf("first note must be the idle warning: %q", all[0])
	}
	if !strings.Contains(all[1], "[Subagent Closed: sub]") || !strings.Contains(all[1], "of inactivity without a done signal") {
		t.Fatalf("close note wrong: %q", all[1])
	}
	if entry := j.tool.tracker.GetSubagent("s1"); entry == nil || entry.Status != scheddomain.SubagentCompleted {
		t.Fatalf("idle teardown must record the completed status, got %+v", entry)
	}
}

// TestInteractiveSubagentJob_IdleTimeoutCarriesLastMessage checks that the close
// note is self-contained: whatever the subagent last harvested comes along.
func TestInteractiveSubagentJob_IdleTimeoutCarriesLastMessage(t *testing.T) {
	j := fastInteractiveJobWithTimeout(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{Harvested: "the subagent's real answer", Screen: "frozen idle prompt"}
	}, 30*time.Millisecond)
	result, notes := runJobCollecting(j, context.Background())

	select {
	case <-result:
	case <-time.After(2 * time.Second):
		t.Fatalf("the idle timeout must close the subagent and return")
	}

	all := notes()
	if len(all) != 2 {
		t.Fatalf("emitted %d notes, want the per-turn completion plus the close note: %v", len(all), all)
	}
	if !strings.Contains(all[1], "[Subagent Closed: sub]") || !strings.Contains(all[1], "the subagent's real answer") {
		t.Fatalf("close note must carry the last harvested message: %q", all[1])
	}
}

// TestInteractiveSubagentJob_ApprovalPausesIdleTimeout guards the clock: time
// spent blocked on an approval prompt never counts as inactivity, so a pending
// approval is never closed out from under the approver.
func TestInteractiveSubagentJob_ApprovalPausesIdleTimeout(t *testing.T) {
	j := fastInteractiveJobWithTimeout(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{AwaitingApproval: true, ApprovalSummary: "Bash(rm -rf /tmp/x)"}
	}, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, notes := runJobCollecting(j, ctx)

	select {
	case res := <-result:
		t.Fatalf("approval must pause the idle clock, subagent closed: %+v", res)
	case <-time.After(80 * time.Millisecond):
	}
	all := notes()
	if len(all) != 1 || !strings.Contains(all[0], "Awaiting Approval") {
		t.Fatalf("expected just the approval note, got %v", all)
	}
}

// TestInteractiveSubagentJob_InputKeepsPaneAlive checks the other reset path:
// typing into the pane (SendSubagentInput) changes the screen tail on every
// poll, which keeps the idle clock pushed out, so the pane never hits the close.
func TestInteractiveSubagentJob_InputKeepsPaneAlive(t *testing.T) {
	var calls int
	var mu sync.Mutex
	j := fastInteractiveJobWithTimeout(func() scheddomain.PaneObservation {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return scheddomain.PaneObservation{Screen: fmt.Sprintf("pane after input, tick %d", calls)}
	}, 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, _ := runJobCollecting(j, ctx)

	select {
	case res := <-result:
		t.Fatalf("screen activity must pause the idle clock, subagent closed: %+v", res)
	case <-time.After(80 * time.Millisecond):
	}
}

func TestInteractiveSubagentJob_AwaitingApprovalEmittedOnce(t *testing.T) {
	j := fastInteractiveJob(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{AwaitingApproval: true, ApprovalSummary: "Bash(rm -rf /tmp/x)"}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, notes := runJobCollecting(j, ctx)

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
	j := fastInteractiveJob(func() scheddomain.PaneObservation {
		return scheddomain.PaneObservation{Gone: true}
	})
	done := make(chan agentdomain.ToolExecutionResult, 1)
	go func() { done <- j.Run(context.Background(), func(scheddomain.JobSignal) {}) }()

	select {
	case res := <-done:
		if !res.Success {
			t.Fatalf("expected success result when pane closes, got %+v", res)
		}
		if entry := j.tool.tracker.GetSubagent("s1"); entry == nil || entry.Status != scheddomain.SubagentCompleted {
			t.Fatalf("pane-gone teardown must record the completed status, got %+v", entry)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Run did not return when the pane was gone")
	}
}

// TestInteractiveSubagentJob_IdleTimeoutFromConfig checks the config wiring: the
// constructor folds tools.agent.idle_timeout into the monitor's clock, seconds to
// duration, and an explicit 0 disables the auto-close.
func TestInteractiveSubagentJob_IdleTimeoutFromConfig(t *testing.T) {
	state := &scheddomain.SubagentState{ID: "s1", SessionID: "sess", StartedAt: time.Now()}
	tool := &AgentTool{tracker: schedinfra.NewSubagentTracker()}

	tool.config = config.DefaultConfig()
	if j := newInteractiveSubagentJob(tool, state); j.idleTimeout != 300*time.Second {
		t.Fatalf("default idle timeout = %v, want 300s", j.idleTimeout)
	}

	tool.config.Tools.Agent.IdleTimeout = 0
	if j := newInteractiveSubagentJob(tool, state); j.idleTimeout != 0 {
		t.Fatalf("idle_timeout 0 must disable the auto-close, got %v", j.idleTimeout)
	}

	tool.config.Tools.Agent.IdleTimeout = 7
	if j := newInteractiveSubagentJob(tool, state); j.idleTimeout != 7*time.Second {
		t.Fatalf("idle timeout = %v, want 7s", j.idleTimeout)
	}
}
