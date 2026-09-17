package headless

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

func TestIsBashTask(t *testing.T) {
	for task, want := range map[string]bool{
		"!ls -la":          true,
		"  !pwd":           true,
		`!!Read(path="x")`: false,
		"list files":       false,
	} {
		if got := isBashTask(task); got != want {
			t.Errorf("isBashTask(%q) = %v, want %v", task, got, want)
		}
	}
}

func TestDirectCall(t *testing.T) {
	cases := map[string]struct {
		task   string
		direct bool
		name   string
		args   string
		err    bool
	}{
		"tool":          {task: `!!Read(file_path="README.md")`, direct: true, name: "Read", args: `{"file_path":"README.md"}`},
		"tool json":     {task: `!!Tree({"path":".","max_depth":2})`, direct: true, name: "Tree", args: `{"max_depth":2,"path":"."}`},
		"bash":          {task: "!ls -la", direct: true, name: "Bash", args: `{"command":"ls -la"}`},
		"bash padded":   {task: "  ! echo hi ", direct: true, name: "Bash", args: `{"command":"echo hi"}`},
		"bash empty":    {task: "!", direct: true, err: true},
		"tool bad":      {task: "!!Read", direct: true, err: true},
		"plain prompt":  {task: "list the files", direct: false},
		"bang in prose": {task: "wow! do it", direct: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fn, direct, err := directCall(tc.task)
			if direct != tc.direct {
				t.Fatalf("direct: want %v got %v", tc.direct, direct)
			}
			if (err != nil) != tc.err {
				t.Fatalf("err: want %v got %v", tc.err, err)
			}
			if err != nil || !direct {
				return
			}
			if fn.Name != tc.name || fn.Arguments != tc.args {
				t.Fatalf("got %s %s", fn.Name, fn.Arguments)
			}
		})
	}
}

func TestRunDirectCallRecordsAndStreams(t *testing.T) {
	store := storage.NewMemoryStorage()
	repo := conversation.NewPersistentConversationRepository(nil, nil, store)
	repo.SetConversationID("sess-1")
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(true)
	tools.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{ToolName: "Bash", Success: true, Data: "hi"}, nil)
	fn := sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"echo hi"}`}

	out := captureStdout(t, func() {
		if err := runDirectCall(context.Background(), "ag-ui", tools, repo, "sess-1", "m", &config.Config{}, fn); err != nil {
			t.Fatalf("runDirectCall: %v", err)
		}
	})

	if tools.ExecuteToolDirectCallCount() != 1 {
		t.Fatalf("tool not executed")
	}
	entries, _, err := store.LoadConversation(context.Background(), "sess-1")
	if err != nil || len(entries) != 2 {
		t.Fatalf("want 2 persisted entries, got %d (%v)", len(entries), err)
	}
	if entries[0].Message.Role != sdk.Assistant || entries[1].Message.Role != sdk.Tool {
		t.Fatalf("unexpected roles: %s %s", entries[0].Message.Role, entries[1].Message.Role)
	}
	for _, want := range []string{"RUN_STARTED", "TOOL_CALL_START", "TOOL_CALL_RESULT", "RUN_FINISHED"} {
		if !strings.Contains(out, want) {
			t.Fatalf("ag-ui stream missing %s:\n%s", want, out)
		}
	}
}

func TestRunDirectCallRejectsDisabledTool(t *testing.T) {
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(false)
	repo := conversation.NewInMemoryConversationRepository(nil, nil)
	err := runDirectCall(context.Background(), "text", tools, repo, "s", "m", &config.Config{}, sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"})
	if err == nil || tools.ExecuteToolDirectCallCount() != 0 {
		t.Fatalf("disabled tool must error before running, err=%v", err)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}
