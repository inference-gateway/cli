package tools

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
)

func TestSendSubagentInputTool_Validate(t *testing.T) {
	tool := NewSendSubagentInputTool(config.DefaultConfig(), schedinfra.NewSubagentTracker())
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

	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "s1", Mode: scheddomain.SubagentModeInteractive, PaneID: "%2",
		SessionID: sessionID, Status: scheddomain.SubagentCompleted,
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
	if s := tracker.GetSubagent("s1"); s == nil || s.Status != scheddomain.SubagentRunning {
		t.Fatalf("submit should re-arm by flipping status back to running, got %v", s)
	}
	if _, err := os.Stat(subagentResultFilePath(sessionID)); !os.IsNotExist(err) {
		t.Fatalf("submit should remove the stale result file, stat err = %v", err)
	}
}

// Sending keys with submit=false drives the TUI without pressing Enter and does
// NOT re-arm (the agent observes via ReadSubagentScreen).
func TestSendSubagentInputTool_KeysNoSubmitDoesNotRearm(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "s2", Mode: scheddomain.SubagentModeInteractive, PaneID: "%3",
		SessionID: "sess-send-keys", Status: scheddomain.SubagentCompleted,
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
	if s := tracker.GetSubagent("s2"); s == nil || s.Status != scheddomain.SubagentCompleted {
		t.Fatalf("submit=false must not re-arm; status changed to %v", s.Status)
	}
}

func TestSendSubagentInputTool_HeadlessSendsText(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	var sent string
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "h1", Mode: scheddomain.SubagentModeHeadless, Status: scheddomain.SubagentCompleted,
		Input: func(text string) error { sent = text; return nil },
	})
	tool := NewSendSubagentInputTool(config.DefaultConfig(), tracker)

	res, err := tool.Execute(context.Background(), map[string]any{"subagent_id": "h1", "text": "also cover X"})
	if err != nil || !res.Success {
		t.Fatalf("Execute: res=%+v err=%v", res, err)
	}
	if sent != "also cover X" {
		t.Fatalf("sent %q, want the text", sent)
	}
	if msg := tool.FormatForLLM(res); !strings.Contains(msg, "notified automatically") {
		t.Fatalf("the model must be told to wait, got %q", msg)
	}
}

func TestSendSubagentInputTool_HeadlessRejectsTUIInput(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"keys", map[string]any{"subagent_id": "h1", "keys": []any{"Enter"}}, "interactive-only"},
		{"submit=false", map[string]any{"subagent_id": "h1", "text": "x", "submit": false}, "interactive-only"},
		{"no input func", map[string]any{"subagent_id": "h2", "text": "x"}, "accepts no message"},
		{"send error", map[string]any{"subagent_id": "h3", "text": "x"}, "has exited"},
	}
	tracker := schedinfra.NewSubagentTracker()
	calls := 0
	_ = tracker.AddSubagent(&scheddomain.SubagentState{ID: "h1", Mode: scheddomain.SubagentModeHeadless, Input: func(string) error { calls++; return nil }})
	_ = tracker.AddSubagent(&scheddomain.SubagentState{ID: "h2", Mode: scheddomain.SubagentModeHeadless})
	_ = tracker.AddSubagent(&scheddomain.SubagentState{ID: "h3", Mode: scheddomain.SubagentModeHeadless, Input: func(string) error { return errors.New("subagent has exited") }})
	tool := NewSendSubagentInputTool(config.DefaultConfig(), tracker)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tool.Execute(context.Background(), tt.args)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if res.Success || !strings.Contains(res.Error, tt.want) {
				t.Fatalf("res = %+v, want a failure mentioning %q", res, tt.want)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("rejected input must not reach the subagent, got %d calls", calls)
	}
}

func TestSendSubagentInputTool_GonePaneFails(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "g1", Mode: scheddomain.SubagentModeInteractive, PaneID: "%9",
		SessionID: "sess-gone", Status: scheddomain.SubagentRunning,
	})
	tool := NewSendSubagentInputTool(config.DefaultConfig(), tracker)
	tool.paneState = func(_ context.Context, _ string) paneState { return paneGone }

	res, _ := tool.Execute(context.Background(), map[string]any{"subagent_id": "g1", "text": "hi"})
	if res.Success {
		t.Fatalf("a gone pane should fail")
	}
}
