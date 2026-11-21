package tools

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
)

func newMessagingAgentTool(tracker scheddomain.SubagentTracker) *AgentTool {
	return NewAgentTool(config.DefaultConfig(), tracker, nil)
}

func TestAgentMessage_Validate(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"nothing to send", map[string]any{"to": "x"}, "provide the 'message'"},
		{"an unsupported key", map[string]any{"to": "x", "keys": []any{"Nope"}}, "unsupported key"},
		{"a spawn beside it", map[string]any{"to": "x", "message": "hi", "description": "spawn"}, "cannot be combined"},
		{"tasks beside it", map[string]any{"to": "x", "message": "hi", "tasks": []any{}}, "cannot be combined"},
		{"named keys", map[string]any{"to": "x", "keys": []any{"Down", "Enter"}}, ""},
		{"a message", map[string]any{"to": "x", "message": "hi"}, ""},
	}
	tool := newMessagingAgentTool(schedinfra.NewSubagentTracker())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want it valid", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate = %v, want an error mentioning %q", err, tt.want)
			}
		})
	}
}

// Submitting a prompt sends text + Enter and RE-ARMS the watcher: the stale
// result file is dropped and the subagent flips back to running so the poller
// re-notifies on completion.
func TestAgentMessage_SubmitRearms(t *testing.T) {
	sessionID := "sess-send-rearm"
	t.Cleanup(func() { _ = os.Remove(subagentResultFilePath(sessionID)) })
	writeTestResultFile(t, sessionID, "old answer")

	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "s1", Mode: scheddomain.SubagentModeInteractive, PaneID: "%2",
		SessionID: sessionID, Status: scheddomain.SubagentCompleted,
	})
	tool := newMessagingAgentTool(tracker)
	tool.paneState = func(_ context.Context, _ string) paneState { return paneAlive }
	var gotText string
	var gotKeys []string
	tool.sendKeys = func(_ context.Context, _, text string, keys []string) error {
		gotText, gotKeys = text, keys
		return nil
	}

	res, err := tool.Execute(context.Background(), map[string]any{"to": "s1", "message": "do more"})
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
func TestAgentMessage_KeysNoSubmitDoesNotRearm(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "s2", Mode: scheddomain.SubagentModeInteractive, PaneID: "%3",
		SessionID: "sess-send-keys", Status: scheddomain.SubagentCompleted,
	})
	tool := newMessagingAgentTool(tracker)
	tool.paneState = func(_ context.Context, _ string) paneState { return paneAlive }
	var gotKeys []string
	tool.sendKeys = func(_ context.Context, _, _ string, keys []string) error {
		gotKeys = keys
		return nil
	}

	res, err := tool.Execute(context.Background(), map[string]any{"to": "s2", "keys": []any{"Down", "Down"}, "submit": false})
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

func TestAgentMessage_HeadlessSendsText(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	var sent string
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "h1", Label: "researcher", Mode: scheddomain.SubagentModeHeadless, Status: scheddomain.SubagentCompleted,
		Input: func(text string) error { sent = text; return nil },
	})
	tool := newMessagingAgentTool(tracker)

	res, err := tool.Execute(context.Background(), map[string]any{"to": "researcher", "message": "also cover X"})
	if err != nil || !res.Success {
		t.Fatalf("Execute: res=%+v err=%v", res, err)
	}
	if sent != "also cover X" {
		t.Fatalf("sent %q, want the text", sent)
	}
	if msg := tool.FormatForLLM(res); !strings.Contains(msg, "notified automatically") {
		t.Fatalf("the model must be told to wait, got %q", msg)
	}
	if preview := tool.FormatPreview(res); preview != "Messaged researcher" {
		t.Fatalf("preview = %q", preview)
	}
}

func TestAgentMessage_HeadlessRejectsTUIInput(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"keys", map[string]any{"to": "h1", "keys": []any{"Enter"}}, "interactive-only"},
		{"submit=false", map[string]any{"to": "h1", "message": "x", "submit": false}, "interactive-only"},
		{"no input func", map[string]any{"to": "h2", "message": "x"}, "accepts no message"},
		{"send error", map[string]any{"to": "h3", "message": "x"}, "has exited"},
	}
	tracker := schedinfra.NewSubagentTracker()
	calls := 0
	_ = tracker.AddSubagent(&scheddomain.SubagentState{ID: "h1", Mode: scheddomain.SubagentModeHeadless, Input: func(string) error { calls++; return nil }})
	_ = tracker.AddSubagent(&scheddomain.SubagentState{ID: "h2", Mode: scheddomain.SubagentModeHeadless})
	_ = tracker.AddSubagent(&scheddomain.SubagentState{ID: "h3", Mode: scheddomain.SubagentModeHeadless, Input: func(string) error { return errors.New("subagent has exited") }})
	tool := newMessagingAgentTool(tracker)
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

func TestAgentMessage_GonePaneFails(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{
		ID: "g1", Mode: scheddomain.SubagentModeInteractive, PaneID: "%9",
		SessionID: "sess-gone", Status: scheddomain.SubagentRunning,
	})
	tool := newMessagingAgentTool(tracker)
	tool.paneState = func(_ context.Context, _ string) paneState { return paneGone }

	res, _ := tool.Execute(context.Background(), map[string]any{"to": "g1", "message": "hi"})
	if res.Success {
		t.Fatalf("a gone pane should fail")
	}
}

func TestAgentMessage_ATeammatePostsToItsLead(t *testing.T) {
	tests := []struct {
		name  string
		args  map[string]any
		posts []scheddomain.TeamMessage
		want  string
	}{
		{
			name:  "to the lead",
			args:  map[string]any{"to": "lead", "message": "found it"},
			posts: []scheddomain.TeamMessage{{To: "lead", Text: "found it"}},
		},
		{
			name:  "to a teammate, which the lead relays",
			args:  map[string]any{"to": "reviewer", "message": "check main.go"},
			posts: []scheddomain.TeamMessage{{To: "reviewer", Text: "check main.go"}},
		},
		{
			name: "pane keys, which only reach a subagent of its own",
			args: map[string]any{"to": "reviewer", "keys": []any{"Enter"}},
			want: "drive the pane of a subagent you spawned",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lead := &schedmocks.FakeTeamChannel{}
			ctx := scheddomain.WithTeamChannel(context.Background(), lead)
			res, err := newMessagingAgentTool(schedinfra.NewSubagentTracker()).Execute(ctx, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			var posted []scheddomain.TeamMessage
			for i := range lead.PostCallCount() {
				posted = append(posted, lead.PostArgsForCall(i))
			}
			if tt.want != "" {
				if res.Success || !strings.Contains(res.Error, tt.want) || len(posted) != 0 {
					t.Fatalf("res = %+v posted %v, want a refusal mentioning %q", res, posted, tt.want)
				}
				return
			}
			if !res.Success || len(posted) != 1 || posted[0] != tt.posts[0] {
				t.Fatalf("res = %+v posted %v, want %v", res, posted, tt.posts)
			}
		})
	}
}

func TestAgentMessage_WithoutALeadNamesWhoCanBeReached(t *testing.T) {
	tracker := schedinfra.NewSubagentTracker()
	_ = tracker.AddSubagent(&scheddomain.SubagentState{ID: "h1", Label: "researcher", Mode: scheddomain.SubagentModeHeadless})
	res, err := newMessagingAgentTool(tracker).Execute(context.Background(), map[string]any{"to": "lead", "message": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || !strings.Contains(res.Error, `No agent named "lead". You can message: researcher.`) {
		t.Fatalf("res = %+v, want a refusal naming the subagents", res)
	}
}

func TestAgentTool_RequiresApproval(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		mode agentdomain.AgentMode
		want bool
	}{
		{"a message", map[string]any{"to": "lead", "message": "hi"}, agentdomain.AgentModeStandard, false},
		{"a spawn", map[string]any{"description": "look around"}, agentdomain.AgentModeStandard, true},
		{"a spawn in auto-accept", map[string]any{"description": "look around"}, agentdomain.AgentModeAutoAccept, false},
		{"a spawn by a read-only agent", map[string]any{"description": "look around"}, agentdomain.AgentModeReadOnly, false},
	}
	tool := newMessagingAgentTool(schedinfra.NewSubagentTracker())
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tool.RequiresApproval(tt.args, tt.mode); got != tt.want {
				t.Fatalf("RequiresApproval = %t, want %t", got, tt.want)
			}
		})
	}
}
