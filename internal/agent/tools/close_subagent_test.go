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
	tool := NewCloseSubagentTool(config.DefaultConfig(), utils.NewSubagentTracker())
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
	tool := NewCloseSubagentTool(config.DefaultConfig(), tracker)
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

func TestCloseSubagentTool_HeadlessCancels(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	cancelled := false
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "h1", Mode: domain.SubagentModeHeadless, Status: domain.SubagentRunning,
		CancelFunc: func() { cancelled = true },
	})
	tool := NewCloseSubagentTool(config.DefaultConfig(), tracker)

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
	tool := NewCloseSubagentTool(config.DefaultConfig(), utils.NewSubagentTracker())
	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "nope"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatalf("expected failure for missing subagent")
	}
}
