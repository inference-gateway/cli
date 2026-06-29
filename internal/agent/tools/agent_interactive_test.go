package tools

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

// TestAgentTool_InteractiveTracksPane verifies the fire-and-track behavior: an
// interactive subagent is recorded (with its pane id) as a running subagent and
// Execute returns immediately, even with wait=true.
func TestAgentTool_InteractiveTracksPane(t *testing.T) {
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	cfg := config.DefaultConfig()
	cfg.Tools.Agent.Mode = "interactive"
	cfg.Tools.Agent.Wait = true // interactive ignores wait - it must still return immediately
	tracker := utils.NewSubagentTracker()
	tool := NewAgentTool(cfg, tracker)
	tool.interactiveAvailable = func() bool { return true }
	tool.launchPane = func(ctx context.Context, title, command string) (string, error) { return "%9", nil }
	tool.sendTask = func(ctx context.Context, paneID, task string) error { return nil }

	res, err := tool.Execute(context.Background(), map[string]any{"description": "do x", "label": "worker"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	subs := tracker.GetAllSubagents()
	if len(subs) != 1 {
		t.Fatalf("expected 1 tracked subagent, got %d", len(subs))
	}
	got := subs[0]
	if got.PaneID != "%9" {
		t.Fatalf("PaneID not stored: %q", got.PaneID)
	}
	if got.Mode != domain.SubagentModeInteractive {
		t.Fatalf("mode = %q, want interactive", got.Mode)
	}
	if got.Status != domain.SubagentRunning {
		t.Fatalf("interactive subagent should remain running, got %q", got.Status)
	}
}
