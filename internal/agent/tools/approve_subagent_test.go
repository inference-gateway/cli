package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	config "github.com/inference-gateway/cli/config"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
)

func writeTestApprovalFile(t *testing.T, sessionID, summary string) {
	t.Helper()
	data, err := json.Marshal(scheddomain.SubagentApprovalFile{Awaiting: true, Summary: summary})
	if err != nil {
		t.Fatalf("marshal approval file: %v", err)
	}
	if err := os.WriteFile(subagentApprovalFilePath(sessionID), data, 0o600); err != nil {
		t.Fatalf("write approval file: %v", err)
	}
}

func TestApproveSubagentTool_Validate(t *testing.T) {
	tool := NewApproveSubagentTool(config.DefaultConfig(), schedinfra.NewSubagentTracker())
	if err := tool.Validate(map[string]any{"decision": "approve"}); err == nil {
		t.Fatalf("missing subagent_id should error")
	}
	if err := tool.Validate(map[string]any{"subagent_id": "x"}); err == nil {
		t.Fatalf("missing decision should error")
	}
	if err := tool.Validate(map[string]any{"subagent_id": "x", "decision": "maybe"}); err == nil {
		t.Fatalf("invalid decision should error")
	}
	if err := tool.Validate(map[string]any{"subagent_id": "x", "decision": "approve"}); err != nil {
		t.Fatalf("valid args should pass: %v", err)
	}
}

// ApproveSubagent is always approval-gated so the human confirms the relay.
func TestApproveSubagentTool_AlwaysRequiresApproval(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Safety.RequireApproval = false
	if !cfg.IsApprovalRequired("ApproveSubagent") {
		t.Fatalf("ApproveSubagent must always require approval")
	}
}

func TestApproveSubagentTool_ApproveSendsEnterAndClearsSidecar(t *testing.T) {
	sessionID := "sess-approve"
	t.Cleanup(func() { _ = os.Remove(subagentApprovalFilePath(sessionID)) })
	writeTestApprovalFile(t, sessionID, "Bash rm -rf build")

	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "s1", Mode: scheddomain.SubagentModeInteractive, PaneID: "%2",
		SessionID: sessionID, Status: scheddomain.SubagentRunning,
	})
	tool := NewApproveSubagentTool(config.DefaultConfig(), tracker)
	tool.paneState = func(_ context.Context, _ string) paneState { return paneAlive }
	var gotKeys []string
	tool.sendKeys = func(_ context.Context, _, _ string, keys []string) error {
		gotKeys = keys
		return nil
	}

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "s1", "decision": "approve"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Error)
	}
	if len(gotKeys) != 1 || gotKeys[0] != "Enter" {
		t.Fatalf("approve should press Enter, got %v", gotKeys)
	}
	if _, err := os.Stat(subagentApprovalFilePath(sessionID)); !os.IsNotExist(err) {
		t.Fatalf("approve should clear the approval sidecar, stat err = %v", err)
	}
}

func TestApproveSubagentTool_RejectSendsEscape(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "s2", Mode: scheddomain.SubagentModeInteractive, PaneID: "%3",
		SessionID: "sess-reject", Status: scheddomain.SubagentRunning,
	})
	tool := NewApproveSubagentTool(config.DefaultConfig(), tracker)
	tool.paneState = func(_ context.Context, _ string) paneState { return paneAlive }
	var gotKeys []string
	tool.sendKeys = func(_ context.Context, _, _ string, keys []string) error {
		gotKeys = keys
		return nil
	}

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "s2", "decision": "reject"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Error)
	}
	if len(gotKeys) != 1 || gotKeys[0] != "Escape" {
		t.Fatalf("reject should press Escape, got %v", gotKeys)
	}
}

func TestApproveSubagentTool_HeadlessFails(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "h1", Mode: scheddomain.SubagentModeHeadless, Status: scheddomain.SubagentRunning,
	})
	tool := NewApproveSubagentTool(config.DefaultConfig(), tracker)

	res, _ := tool.Execute(context.Background(), map[string]any{"subagent_id": "h1", "decision": "approve"})
	if res.Success {
		t.Fatalf("headless subagent has no approval prompt and should fail")
	}
}
