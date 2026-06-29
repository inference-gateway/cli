package tools

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

func TestGetSubagentResultTool_Validate(t *testing.T) {
	tool := NewGetSubagentResultTool(config.DefaultConfig(), utils.NewSubagentTracker())
	if err := tool.Validate(map[string]any{}); err == nil {
		t.Fatalf("missing subagent_id should error")
	}
	if err := tool.Validate(map[string]any{"subagent_id": "x"}); err != nil {
		t.Fatalf("valid args should pass: %v", err)
	}
}

// A completed interactive subagent (kept tracked, pane still open) can have its
// final output re-read on demand.
func TestGetSubagentResultTool_CompletedInteractiveReadsPane(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "s1", Label: "w", Mode: domain.SubagentModeInteractive,
		SessionID: "sess", PaneID: "%5", Status: domain.SubagentCompleted,
	})
	tool := NewGetSubagentResultTool(config.DefaultConfig(), tracker)
	gotLines := -1
	tool.capturePane = func(ctx context.Context, paneID string, maxLines int) string {
		gotLines = maxLines
		return "hello from pane"
	}
	tool.paneState = func(ctx context.Context, paneID string) paneState { return paneAlive }

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "s1", "lines": 12})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}
	data := res.Data.(map[string]any)
	if data["output"] != "hello from pane" {
		t.Fatalf("output = %v", data["output"])
	}
	if gotLines != 12 {
		t.Fatalf("lines arg not forwarded: got %d", gotLines)
	}
}

// A running subagent (either mode) must refuse the poll - it notifies automatically.
func TestGetSubagentResultTool_RunningRefuses(t *testing.T) {
	for _, mode := range []string{domain.SubagentModeInteractive, domain.SubagentModeHeadless} {
		tracker := utils.NewSubagentTracker()
		_ = tracker.AddSubagent(&domain.SubagentState{
			ID: "r1", Label: "w", Mode: mode, PaneID: "%5", Status: domain.SubagentRunning,
		})
		tool := NewGetSubagentResultTool(config.DefaultConfig(), tracker)
		tool.capturePane = func(ctx context.Context, paneID string, maxLines int) string { return "x" }
		tool.paneState = func(ctx context.Context, paneID string) paneState { return paneAlive }
		res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "r1"})
		if err != nil {
			t.Fatalf("Execute(%s): %v", mode, err)
		}
		if res.Success {
			t.Fatalf("a running %s subagent should refuse polling", mode)
		}
	}
}

func TestGetSubagentResultTool_NotFound(t *testing.T) {
	tool := NewGetSubagentResultTool(config.DefaultConfig(), utils.NewSubagentTracker())
	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "nope"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatalf("expected failure for missing subagent")
	}
}

// A running headless subagent is monitored in the background and notifies
// automatically, so polling it via GetSubagentResult must be refused.
func TestGetSubagentResultTool_HeadlessRefusesWhileRunning(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "h1", Label: "worker", Mode: domain.SubagentModeHeadless, Status: domain.SubagentRunning,
	})
	tool := NewGetSubagentResultTool(config.DefaultConfig(), tracker)
	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "h1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatalf("a running headless subagent should refuse polling")
	}
	if res.Error == "" {
		t.Fatalf("refusal should explain that the subagent notifies automatically")
	}
}
