package headless

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

func TestEmitPreRunError(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"json", `"agent_error"`},
		{"json-pretty", `"agent_error"`},
		{"ag-ui", `"RUN_ERROR"`},
		{"text", ""},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			var out strings.Builder
			emitPreRunError(&out, tt.format, errors.New("gateway down"))
			if tt.want == "" {
				if out.Len() != 0 {
					t.Fatalf("text format must stay silent, got %q", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), tt.want) || !strings.Contains(out.String(), "gateway down") {
				t.Fatalf("emitPreRunError(%s) output = %q, want %s with the message", tt.format, out.String(), tt.want)
			}
		})
	}
}

func TestValidateOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{"one-shot task", Options{Task: "fix it", Format: "json"}, ""},
		{"serve worker", Options{Serve: true, Format: "ag-ui"}, ""},
		{"unknown format", Options{Task: "fix it", Format: "xml"}, "invalid --format"},
		{"serve with a task", Options{Serve: true, Task: "fix it", Format: "ag-ui"}, "--serve takes no task"},
		{"serve without ag-ui", Options{Serve: true, Format: "json"}, "needs --format ag-ui"},
		{"no task without serve", Options{Format: "json"}, "a task is required"},
		{"keep-alive task", Options{Task: "fix it", Format: "json", KeepAlive: true}, ""},
		{"keep-alive with serve", Options{Serve: true, Format: "ag-ui", KeepAlive: true}, "pick one"},
		{"keep-alive in text", Options{Task: "fix it", Format: "text", KeepAlive: true}, "--format text does not"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOptions(tt.opts)
			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("validateOptions(%+v) = %v, want %q", tt.opts, err, tt.wantErr)
			}
		})
	}
}

// TestSubagentRunStatsCarriesCachedTokens: a headless child's run stats keep the
// prompt-cache slice of its input, so the list under the parent's composer can
// report it beside the totals.
func TestSubagentRunStatsCarriesCachedTokens(t *testing.T) {
	repo := &convmocks.FakeConversationRepository{}
	repo.GetSessionTokensReturns(convdomain.SessionTokenStats{
		TotalInputTokens: 1200, TotalOutputTokens: 80, TotalCachedTokens: 1000,
	})

	want := &scheddomain.SubagentRunStats{InputTokens: 1200, OutputTokens: 80, CachedTokens: 1000}
	if got := subagentRunStats(repo); got == nil || *got != *want {
		t.Fatalf("subagentRunStats = %+v, want %+v", got, want)
	}
}

// A worker spawned as a subagent reports each finished turn to its parent as
// one CUSTOM subagent_turn event. Any other worker reports nothing.
func TestReportSubagentTurn(t *testing.T) {
	repo := &convmocks.FakeConversationRepository{}
	repo.GetMessagesReturns([]convdomain.ConversationEntry{{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("the poem")}}})
	repo.GetSessionTokensReturns(convdomain.SessionTokenStats{TotalInputTokens: 10, TotalOutputTokens: 5})
	queue := &convmocks.FakeMessageQueue{}
	queue.IsEmptyReturns(true)

	var out bytes.Buffer
	t.Setenv(parentSessionEnv, "")
	reportSubagentTurn(&out, repo, queue, "sess", nil)
	if out.Len() != 0 {
		t.Fatalf("a worker without a parent reported %q", out.String())
	}

	t.Setenv(parentSessionEnv, "parent")
	reportSubagentTurn(&out, repo, queue, "sess", errors.New("boom"))
	var frame struct {
		Type  string                       `json:"type"`
		Name  string                       `json:"name"`
		Value scheddomain.SubagentTurnLine `json:"value"`
	}
	if err := json.Unmarshal(out.Bytes(), &frame); err != nil {
		t.Fatalf("decoding %q: %v", out.String(), err)
	}
	if frame.Type != "CUSTOM" || frame.Name != scheddomain.SubagentTurnLineType {
		t.Fatalf("frame = %+v, want a CUSTOM subagent_turn", frame)
	}
	turn := frame.Value.SubagentResultFile
	if turn.FinalAssistant != "the poem" || turn.Success || turn.Error != "boom" || !turn.Done || turn.SessionID != "sess" || turn.Stats.InputTokens != 10 {
		t.Fatalf("turn = %+v", turn)
	}
}
