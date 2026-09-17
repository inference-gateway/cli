package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

func TestCanonicalToolName(t *testing.T) {
	available := []string{"Read", "Bash", "WebSearch"}

	cases := map[string]string{
		"read":      "Read",
		"READ":      "Read",
		"Read":      "Read",
		"bash":      "Bash",
		"websearch": "WebSearch",
		"WebSearch": "WebSearch",
		"unknown":   "unknown",
	}

	for in, want := range cases {
		if got := canonicalToolName(available, in); got != want {
			t.Errorf("canonicalToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderToolResultStripsANSIWhenColorsDisabled(t *testing.T) {
	const styled = "\x1b[38;2;1;2;3m│\x1b[m ok"
	utils.SetColorsDisabled(false)
	if got := renderToolResult(styled); got != styled {
		t.Fatalf("colors enabled: got %q", got)
	}
	utils.SetColorsDisabled(true)
	t.Cleanup(func() { utils.SetColorsDisabled(false) })
	if got := renderToolResult(styled); got != "│ ok" {
		t.Fatalf("colors disabled: got %q", got)
	}
}

func TestRecordToolCallPersistsCallAndResultUnderSessionID(t *testing.T) {
	store := storage.NewMemoryStorage()
	repo := conversation.NewPersistentConversationRepository(nil, nil, store)
	fn := sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"echo hi"}`}
	result := &agentdomain.ToolExecutionResult{ToolName: "Bash", Success: true}

	if err := recordToolCall(context.Background(), repo, "sess-1", fn, result); err != nil {
		t.Fatalf("recordToolCall: %v", err)
	}

	entries, _, err := store.LoadConversation(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Message.Role != sdk.Assistant || entries[0].Message.ToolCalls == nil || (*entries[0].Message.ToolCalls)[0].ID != result.ToolCallID {
		t.Fatalf("assistant entry missing matching tool call: %+v", entries[0].Message)
	}
	if entries[1].Message.Role != sdk.Tool || *entries[1].Message.ToolCallID != result.ToolCallID {
		t.Fatalf("tool entry missing matching tool_call_id: %+v", entries[1].Message)
	}

	if err := recordToolCall(context.Background(), repo, "sess-1", fn, &agentdomain.ToolExecutionResult{ToolName: "Bash"}); err != nil {
		t.Fatalf("second recordToolCall: %v", err)
	}
	entries, _, _ = store.LoadConversation(context.Background(), "sess-1")
	if len(entries) != 4 {
		t.Fatalf("second call must append, want 4 entries, got %d", len(entries))
	}
}

func TestRecordToolCallRejectsNonPersistentRepo(t *testing.T) {
	repo := conversation.NewInMemoryConversationRepository(nil, nil)
	err := recordToolCall(context.Background(), repo, "sess-1", sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"}, &agentdomain.ToolExecutionResult{})
	if err == nil {
		t.Fatal("expected an error when storage is disabled")
	}
}

func execJSON(t *testing.T, args []string, approved bool) execResult {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Tools.Enabled = true
	cfg.Tools.Bash.Enabled = true

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	execErr := ExecTool(cfg, args, "json", "", approved)
	os.Stdout = stdout
	_ = w.Close()
	if execErr != nil {
		t.Fatalf("ExecTool: %v", execErr)
	}
	var got execResult
	if err := json.NewDecoder(r).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

func TestExecToolJSONReportsApprovalRequired(t *testing.T) {
	got := execJSON(t, []string{"Bash", `{"command":"printf unlisted-xyz"}`}, false)
	if !got.ApprovalRequired || got.Success || got.Output != "" {
		t.Fatalf("got %+v, want approval_required only", got)
	}
}

func TestExecToolJSONApprovedRunsUnlistedCommandWithRawOutput(t *testing.T) {
	got := execJSON(t, []string{"Bash", `{"command":"printf unlisted-xyz"}`}, true)
	if got.ApprovalRequired || !got.Success || got.Output != "unlisted-xyz" {
		t.Fatalf("got %+v, want raw successful output", got)
	}
}

func TestExecToolJSONApprovedKeepsOutputOnFailure(t *testing.T) {
	got := execJSON(t, []string{"Bash", `{"command":"printf 'HTTP/2.0 404 Not Found'; exit 1"}`}, true)
	if got.Success || got.Output != "HTTP/2.0 404 Not Found" || got.Error == "" {
		t.Fatalf("got %+v, want failed result with output and error", got)
	}
}
