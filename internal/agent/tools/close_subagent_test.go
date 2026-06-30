package tools

import (
	"context"
	"os"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

func TestCloseSubagentTool_Validate(t *testing.T) {
	tool := NewCloseSubagentTool(config.DefaultConfig(), utils.NewSubagentTracker(), nil)
	if err := tool.Validate(map[string]any{}); err == nil {
		t.Fatalf("missing subagent_id should error")
	}
}

func TestCloseSubagentTool_InteractiveKillsAndHarvests(t *testing.T) {
	sessionID := "sess-close"
	t.Cleanup(func() { _ = os.Remove(subagentResultFilePath(sessionID)) })
	writeTestResultFile(t, sessionID, "final words")

	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "s1", Label: "w", Mode: domain.SubagentModeInteractive,
		SessionID: sessionID, PaneID: "%7", Status: domain.SubagentRunning,
	})
	tool := NewCloseSubagentTool(config.DefaultConfig(), tracker, nil)
	killed := ""
	tool.killPane = func(ctx context.Context, paneID string) error { killed = paneID; return nil }

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "s1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}
	if killed != "%7" {
		t.Fatalf("pane not killed: %q", killed)
	}
	if tracker.GetSubagent("s1") != nil {
		t.Fatalf("interactive subagent should be removed from the tracker")
	}
	data := res.Data.(map[string]any)
	if data["final_output"] != "final words" {
		t.Fatalf("final output not harvested: %v", data["final_output"])
	}
}

// fakeJobStopper records WindJob calls so the close-subagent test can assert the
// supervised monitor is wound down (the fix for "closed subagents still show in
// the status-line count").
type fakeJobStopper struct {
	winds []struct {
		id  string
		sig domain.WindSignal
	}
}

func (f *fakeJobStopper) WindJob(id string, sig domain.WindSignal) error {
	f.winds = append(f.winds, struct {
		id  string
		sig domain.WindSignal
	}{id, sig})
	return nil
}

// TestCloseSubagentTool_InteractiveWindsSupervisedJob is the regression guard for
// the stuck "N subagents" count: closing an interactive subagent must WindStop its
// supervised job (which cancels the job's Run context so the count drops at once),
// not just kill the pane out-from-under a monitor that keeps polling.
func TestCloseSubagentTool_InteractiveWindsSupervisedJob(t *testing.T) {
	sessionID := "sess-wind"
	t.Cleanup(func() { _ = os.Remove(subagentResultFilePath(sessionID)) })

	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "s1", Mode: domain.SubagentModeInteractive,
		SessionID: sessionID, PaneID: "%7", Status: domain.SubagentRunning,
	})
	stopper := &fakeJobStopper{}
	tool := NewCloseSubagentTool(config.DefaultConfig(), tracker, stopper)

	paneKilledDirectly := false
	tool.killPane = func(_ context.Context, _ string) error { paneKilledDirectly = true; return nil }

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "s1"})
	if err != nil || res == nil || !res.Success {
		t.Fatalf("Execute: err=%v res=%+v", err, res)
	}
	if len(stopper.winds) != 1 || stopper.winds[0].id != "s1" || stopper.winds[0].sig != domain.WindStop {
		t.Fatalf("expected one WindJob(s1, WindStop), got %+v", stopper.winds)
	}
	if paneKilledDirectly {
		t.Fatalf("with a supervisor wired, the job's Wind kills the pane - killPane must not be called directly")
	}
	if tracker.GetSubagent("s1") != nil {
		t.Fatalf("subagent should be removed from the tracker")
	}
}

func TestCloseSubagentTool_HeadlessCancels(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	cancelled := false
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "h1", Mode: domain.SubagentModeHeadless, Status: domain.SubagentRunning,
		CancelFunc: func() { cancelled = true },
	})
	tool := NewCloseSubagentTool(config.DefaultConfig(), tracker, nil)

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "h1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}
	if !cancelled {
		t.Fatalf("headless subagent should be cancelled via CancelFunc")
	}
}

func TestCloseSubagentTool_NotFound(t *testing.T) {
	tool := NewCloseSubagentTool(config.DefaultConfig(), utils.NewSubagentTracker(), nil)
	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "nope"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatalf("expected failure for missing subagent")
	}
}
