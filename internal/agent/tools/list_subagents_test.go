package tools

import (
	"context"
	"testing"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"

	config "github.com/inference-gateway/cli/config"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

func TestListSubagentsTool_Definition(t *testing.T) {
	tool := NewListSubagentsTool(config.DefaultConfig(), utils.NewSubagentTracker())
	if def := tool.Definition(); def.Function.Name != "ListSubagents" {
		t.Fatalf("Definition name = %q, want ListSubagents", def.Function.Name)
	}
}

func TestListSubagentsTool_Empty(t *testing.T) {
	tool := NewListSubagentsTool(config.DefaultConfig(), utils.NewSubagentTracker())
	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data := res.Data.(map[string]any)
	if data["subagent_count"].(int) != 0 {
		t.Fatalf("expected 0 subagents, got %v", data["subagent_count"])
	}
}

func TestListSubagentsTool_ReportsLivePaneStatus(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "s1", Label: "worker", Mode: scheddomain.SubagentModeInteractive,
		SessionID: "sess", PaneID: "%3", Status: scheddomain.SubagentRunning,
	})
	tool := NewListSubagentsTool(config.DefaultConfig(), tracker)
	tool.paneState = func(ctx context.Context, paneID string) paneState {
		if paneID == "%3" {
			return paneDead
		}
		return paneGone
	}

	res, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data := res.Data.(map[string]any)
	if data["subagent_count"].(int) != 1 {
		t.Fatalf("want 1 subagent, got %v", data["subagent_count"])
	}
	infos := data["subagents"].([]map[string]any)
	if infos[0]["status"] != "finished" {
		t.Fatalf("a dead pane should report status 'finished', got %v", infos[0]["status"])
	}
	if infos[0]["pane_id"] != "%3" {
		t.Fatalf("want pane_id %%3, got %v", infos[0]["pane_id"])
	}
	if infos[0]["subagent_id"] != "s1" {
		t.Fatalf("want subagent_id s1, got %v", infos[0]["subagent_id"])
	}
}
