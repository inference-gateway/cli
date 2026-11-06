package headless

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
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

func TestQueuedDirectCall(t *testing.T) {
	queued := func(texts ...string) []convdomain.QueuedMessage {
		turn := make([]convdomain.QueuedMessage, 0, len(texts))
		for _, text := range texts {
			turn = append(turn, convdomain.QueuedMessage{Message: userMessageText(text)})
		}
		return turn
	}

	if _, direct, _ := queuedDirectCall(nil); direct {
		t.Fatal("an empty turn is no direct task")
	}

	fn, direct, err := queuedDirectCall(queued("!echo hi"))
	if !direct || err != nil || fn.Name != "Bash" || fn.Arguments != `{"command":"echo hi"}` {
		t.Fatalf("one !cmd message: direct=%v err=%v fn=%+v", direct, err, fn)
	}

	if _, direct, err := queuedDirectCall(queued("!!Read")); !direct || err == nil {
		t.Fatalf("a malformed tool call is a direct task with an error: direct=%v err=%v", direct, err)
	}

	if _, direct, _ := queuedDirectCall(queued("!ls", "and explain")); direct {
		t.Fatal("a turn of several messages goes to the agent")
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
		"bash detached": {task: "!sleep 8 &", direct: true, name: "Bash", args: `{"command":"sleep 8","detached":true}`},
		"bash empty":    {task: "!", direct: true, err: true},
		"bash only &":   {task: "! &", direct: true, err: true},
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

func TestRunDirectCallRecordsWhatTheChatTUIRecords(t *testing.T) {
	tests := []struct {
		name      string
		task      string
		fn        sdk.ChatCompletionMessageToolCallFunction
		roles     []sdk.MessageRole
		callID    string
		modelSees int
	}{
		{
			name:      "a !command reaches the model as its hidden output",
			task:      "!echo hi",
			fn:        sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"echo hi"}`},
			roles:     []sdk.MessageRole{sdk.User, sdk.Assistant, sdk.Tool, sdk.User},
			callID:    "user-bash-",
			modelSees: 2,
		},
		{
			name:      "a !!Tool keeps its tool pair",
			task:      `!!Read(file_path="README.md")`,
			fn:        sdk.ChatCompletionMessageToolCallFunction{Name: "Read", Arguments: `{"file_path":"README.md"}`},
			roles:     []sdk.MessageRole{sdk.User, sdk.Assistant, sdk.Tool},
			callID:    "user-tool-",
			modelSees: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := storage.NewMemoryStorage()
			repo := conversation.NewPersistentConversationRepository(nil, nil, store)
			repo.SetConversationID("sess-1")
			tools := &agentdomainmocks.FakeToolService{}
			tools.IsToolEnabledReturns(true)
			tools.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{ToolName: tt.fn.Name, Success: true, Data: "hi"}, nil)

			captureStdout(t, func() {
				if err := runDirectCall(context.Background(), "ag-ui", tools, repo, "sess-1", "m", &config.Config{}, tt.task, tt.fn); err != nil {
					t.Fatalf("runDirectCall: %v", err)
				}
			})

			entries, _, err := store.LoadConversation(context.Background(), "sess-1")
			if err != nil || len(entries) != len(tt.roles) {
				t.Fatalf("want %d persisted entries, got %d (%v)", len(tt.roles), len(entries), err)
			}
			for i, role := range tt.roles {
				if entries[i].Message.Role != role {
					t.Errorf("entry %d has role %s, want %s", i, entries[i].Message.Role, role)
				}
			}
			if typed, _ := entries[0].Message.Content.AsMessageContent0(); typed != tt.task {
				t.Errorf("first entry = %q, want the typed task", typed)
			}
			if entries[2].Message.ToolCallID == nil || !strings.HasPrefix(*entries[2].Message.ToolCallID, tt.callID) {
				t.Errorf("tool entry id = %v, want the %s prefix", entries[2].Message.ToolCallID, tt.callID)
			}
			if got := len(convdomain.BuildAgentMessagesFromEntries(entries)); got != tt.modelSees {
				t.Errorf("the model sees %d messages, want %d", got, tt.modelSees)
			}
		})
	}
}

func TestRunDirectCallRendersTheOutputWhileTheCommandRuns(t *testing.T) {
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(true)
	tools.ExecuteToolDirectStub = func(ctx context.Context, _ sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
		agentdomain.GetBashOutputCallback(ctx)("line 1")
		return &agentdomain.ToolExecutionResult{ToolName: "Bash", Success: true}, nil
	}
	repo := conversation.NewInMemoryConversationRepository(nil, nil)

	out := captureStdout(t, func() {
		if err := runDirectCall(t.Context(), "ag-ui", tools, repo, "s", "m", &config.Config{}, "!echo hi", sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"echo hi"}`}); err != nil {
			t.Fatalf("runDirectCall: %v", err)
		}
	})

	position := 0
	for _, want := range []string{"RUN_STARTED", "TOOL_CALL_START", "tool_progress", "line 1", "TOOL_CALL_RESULT", "RUN_FINISHED"} {
		at := strings.Index(out[position:], want)
		if at < 0 {
			t.Fatalf("%s is missing or out of order in the stream:\n%s", want, out)
		}
		position += at
	}
}

func TestRunDirectCallDropsWhatATooLateCallbackReports(t *testing.T) {
	var late agentdomain.BashOutputCallback
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(true)
	tools.ExecuteToolDirectStub = func(ctx context.Context, _ sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
		late = agentdomain.GetBashOutputCallback(ctx)
		return &agentdomain.ToolExecutionResult{ToolName: "Bash", Success: true}, nil
	}
	repo := conversation.NewInMemoryConversationRepository(nil, nil)

	captureStdout(t, func() {
		if err := runDirectCall(t.Context(), "ag-ui", tools, repo, "s", "m", &config.Config{}, "!sleep 8 &", sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"sleep 8","detached":true}`}); err != nil {
			t.Fatalf("runDirectCall: %v", err)
		}
	})

	late("a detached command's last output")
}

func TestRunDirectCallEndsACancelledCommandAsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(true)
	tools.ExecuteToolDirectStub = func(ctx context.Context, _ sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
		cancel()
		<-ctx.Done()
		return &agentdomain.ToolExecutionResult{ToolName: "Bash", Success: false, Error: "command was cancelled and killed"}, nil
	}
	repo := conversation.NewInMemoryConversationRepository(nil, nil)

	var err error
	out := captureStdout(t, func() {
		err = runDirectCall(ctx, "ag-ui", tools, repo, "s", "m", &config.Config{}, "!sleep 100", sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"sleep 100"}`})
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if !strings.Contains(out, `"outcome":{"type":"cancelled"}`) {
		t.Errorf("the run did not end as cancelled:\n%s", out)
	}
	if repo.GetMessageCount() != 4 {
		t.Errorf("recorded %d entries, want what ran still in the conversation", repo.GetMessageCount())
	}
}

func TestRunDirectCallRejectsDisabledTool(t *testing.T) {
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(false)
	repo := conversation.NewInMemoryConversationRepository(nil, nil)
	err := runDirectCall(context.Background(), "text", tools, repo, "s", "m", &config.Config{}, "!ls", sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"})
	if err == nil || tools.ExecuteToolDirectCallCount() != 0 {
		t.Fatalf("disabled tool must error before running, err=%v", err)
	}
	if repo.GetMessageCount() != 0 {
		t.Errorf("a refused task recorded %d entries", repo.GetMessageCount())
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
