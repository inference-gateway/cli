package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

func writeTestResultFile(t *testing.T, sessionID, msg string) {
	t.Helper()
	data, err := json.Marshal(domain.SubagentResultFile{FinalAssistant: msg, Success: true})
	if err != nil {
		t.Fatalf("marshal result file: %v", err)
	}
	if err := os.WriteFile(subagentResultFilePath(sessionID), data, 0o600); err != nil {
		t.Fatalf("write result file: %v", err)
	}
}

func TestGetSubagentResultTool_Validate(t *testing.T) {
	tool := NewGetSubagentResultTool(config.DefaultConfig(), utils.NewSubagentTracker())
	if err := tool.Validate(map[string]any{}); err == nil {
		t.Fatalf("missing subagent_id should error")
	}
	if err := tool.Validate(map[string]any{"subagent_id": "x"}); err != nil {
		t.Fatalf("valid args should pass: %v", err)
	}
}

// A completed interactive subagent returns its real last assistant message from
// the result file - never the pane's chrome.
func TestGetSubagentResultTool_CompletedInteractiveReadsResultFile(t *testing.T) {
	sessionID := "sess-getresult-test"
	t.Cleanup(func() { _ = os.Remove(subagentResultFilePath(sessionID)) })
	writeTestResultFile(t, sessionID, "the real answer")

	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "s1", Label: "w", Mode: domain.SubagentModeInteractive,
		SessionID: sessionID, PaneID: "%5", Status: domain.SubagentCompleted,
	})
	tool := NewGetSubagentResultTool(config.DefaultConfig(), tracker)

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "s1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success")
	}
	data := res.Data.(map[string]any)
	if data["output"] != "the real answer" {
		t.Fatalf("output = %v, want the result-file message", data["output"])
	}
}

// With no result file, the output is empty (never pane chrome).
func TestGetSubagentResultTool_CompletedInteractiveNoFileIsEmpty(t *testing.T) {
	sessionID := "sess-getresult-empty"
	_ = os.Remove(subagentResultFilePath(sessionID))
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "s2", Mode: domain.SubagentModeInteractive,
		SessionID: sessionID, PaneID: "%6", Status: domain.SubagentCompleted,
	})
	tool := NewGetSubagentResultTool(config.DefaultConfig(), tracker)

	res, _ := tool.Execute(context.Background(), map[string]any{"subagent_id": "s2"})
	data := res.Data.(map[string]any)
	if data["output"] != "" {
		t.Fatalf("expected empty output (no pane chrome), got %q", data["output"])
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
