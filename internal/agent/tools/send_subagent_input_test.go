package tools

import (
	"context"
	"os"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

func TestSendSubagentInputTool_Validate(t *testing.T) {
	tool := NewSendSubagentInputTool(config.DefaultConfig(), utils.NewSubagentTracker())
	if err := tool.Validate(map[string]any{}); err == nil {
		t.Fatalf("missing subagent_id should error")
	}
	if err := tool.Validate(map[string]any{"subagent_id": "x"}); err == nil {
		t.Fatalf("missing text and keys should error")
	}
	if err := tool.Validate(map[string]any{"subagent_id": "x", "keys": []any{"Nope"}}); err == nil {
		t.Fatalf("unsupported key should error")
	}
	if err := tool.Validate(map[string]any{"subagent_id": "x", "keys": []any{"Down", "Enter"}}); err != nil {
		t.Fatalf("valid keys should pass: %v", err)
	}
}

// Submitting a prompt sends text + Enter and RE-ARMS the watcher: the stale
// result file is dropped and the subagent flips back to running so the poller
// re-notifies on completion.
func TestSendSubagentInputTool_SubmitRearms(t *testing.T) {
	sessionID := "sess-send-rearm"
	t.Cleanup(func() { _ = os.Remove(subagentResultFilePath(sessionID)) })
	writeTestResultFile(t, sessionID, "old answer")

	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "s1", Mode: domain.SubagentModeInteractive, PaneID: "%2",
		SessionID: sessionID, Status: domain.SubagentCompleted,
	})
	tool := NewSendSubagentInputTool(config.DefaultConfig(), tracker)
	tool.paneState = func(_ context.Context, _ string) paneState { return paneAlive }
	var gotText string
	var gotKeys []string
	tool.sendKeys = func(_ context.Context, _, text string, keys []string) error {
		gotText, gotKeys = text, keys
		return nil
	}

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "s1", "text": "do more"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Error)
	}
	if gotText != "do more" || len(gotKeys) == 0 || gotKeys[len(gotKeys)-1] != "Enter" {
		t.Fatalf("submit should type text then press Enter; text=%q keys=%v", gotText, gotKeys)
	}
	if s := tracker.GetSubagent("s1"); s == nil || s.Status != domain.SubagentRunning {
		t.Fatalf("submit should re-arm by flipping status back to running, got %v", s)
	}
	if _, err := os.Stat(subagentResultFilePath(sessionID)); !os.IsNotExist(err) {
		t.Fatalf("submit should remove the stale result file, stat err = %v", err)
	}
}

// Sending keys with submit=false drives the TUI without pressing Enter and does
// NOT re-arm (the agent observes via ReadSubagentScreen).
func TestSendSubagentInputTool_KeysNoSubmitDoesNotRearm(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "s2", Mode: domain.SubagentModeInteractive, PaneID: "%3",
		SessionID: "sess-send-keys", Status: domain.SubagentCompleted,
	})
	tool := NewSendSubagentInputTool(config.DefaultConfig(), tracker)
	tool.paneState = func(_ context.Context, _ string) paneState { return paneAlive }
	var gotKeys []string
	tool.sendKeys = func(_ context.Context, _, _ string, keys []string) error {
		gotKeys = keys
		return nil
	}

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "s2", "keys": []any{"Down", "Down"}, "submit": false})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Error)
	}
	for _, k := range gotKeys {
		if k == "Enter" {
			t.Fatalf("submit=false must not append Enter; keys=%v", gotKeys)
		}
	}
	if s := tracker.GetSubagent("s2"); s == nil || s.Status != domain.SubagentCompleted {
		t.Fatalf("submit=false must not re-arm; status changed to %v", s.Status)
	}
}

func TestSendSubagentInputTool_HeadlessFails(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "h1", Mode: domain.SubagentModeHeadless, Status: domain.SubagentRunning,
	})
	tool := NewSendSubagentInputTool(config.DefaultConfig(), tracker)

	res, _ := tool.Execute(context.Background(), map[string]any{"subagent_id": "h1", "text": "hi"})
	if res.Success {
		t.Fatalf("headless subagent has no TUI and should fail")
	}
}

func TestSendSubagentInputTool_GonePaneFails(t *testing.T) {
	tracker := utils.NewSubagentTracker()
	_ = tracker.AddSubagent(&domain.SubagentState{
		ID: "g1", Mode: domain.SubagentModeInteractive, PaneID: "%9",
		SessionID: "sess-gone", Status: domain.SubagentRunning,
	})
	tool := NewSendSubagentInputTool(config.DefaultConfig(), tracker)
	tool.paneState = func(_ context.Context, _ string) paneState { return paneGone }

	res, _ := tool.Execute(context.Background(), map[string]any{"subagent_id": "g1", "text": "hi"})
	if res.Success {
		t.Fatalf("a gone pane should fail")
	}
}
