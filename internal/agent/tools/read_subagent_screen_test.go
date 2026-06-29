package tools

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

func TestReadSubagentScreenTool_Validate(t *testing.T) {
	tool := NewReadSubagentScreenTool(config.DefaultConfig(), utils.NewSubagentTracker())
	if err := tool.Validate(map[string]any{}); err == nil {
		t.Fatalf("missing subagent_id should error")
	}
}

// ReadSubagentScreen returns the raw pane content and, unlike GetSubagentResult,
// does NOT refuse while the subagent is running (live TUI snapshots are the point).
func TestReadSubagentScreenTool_CapturesRunningInteractivePane(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "s1", Mode: domain.SubagentModeInteractive, PaneID: "%4",
		SessionID: "sess", Status: domain.SubagentRunning,
	})
	tool := NewReadSubagentScreenTool(config.DefaultConfig(), tracker)
	tool.capture = func(_ context.Context, paneID string, _ int) string {
		if paneID != "%4" {
			t.Fatalf("paneID = %q, want %%4", paneID)
		}
		return "RAW SCREEN"
	}

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "s1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got error %q", res.Error)
	}
	if data := res.Data.(map[string]any); data["screen"] != "RAW SCREEN" {
		t.Fatalf("screen = %v, want RAW SCREEN", data["screen"])
	}
}

func TestReadSubagentScreenTool_HeadlessErrors(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "h1", Mode: domain.SubagentModeHeadless, Status: domain.SubagentRunning,
	})
	tool := NewReadSubagentScreenTool(config.DefaultConfig(), tracker)

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "h1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatalf("headless subagent has no screen and should fail")
	}
}

func TestReadSubagentScreenTool_NotFound(t *testing.T) {
	tool := NewReadSubagentScreenTool(config.DefaultConfig(), utils.NewSubagentTracker())
	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "nope"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatalf("expected failure for missing subagent")
	}
}
