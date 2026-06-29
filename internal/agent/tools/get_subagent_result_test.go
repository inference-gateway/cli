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

func TestGetSubagentResultTool_Interactive(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "s1", Label: "w", Mode: domain.SubagentModeInteractive,
		SessionID: "sess", PaneID: "%5", Status: domain.SubagentRunning,
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
	if data["status"] != "running" {
		t.Fatalf("status = %v, want running", data["status"])
	}
	if gotLines != 12 {
		t.Fatalf("lines arg not forwarded: got %d", gotLines)
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

func TestGetSubagentResultTool_HeadlessMessage(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "h1", Mode: domain.SubagentModeHeadless, Status: domain.SubagentRunning,
	})
	tool := NewGetSubagentResultTool(config.DefaultConfig(), tracker)
	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "h1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data := res.Data.(map[string]any)
	if _, ok := data["message"]; !ok {
		t.Fatalf("headless subagent should return an explanatory message, got %+v", data)
	}
}
