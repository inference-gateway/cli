package headless

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

func TestLiveThreadIDFollowsTheConversationTheRepoHolds(t *testing.T) {
	tests := []struct {
		name    string
		current string
		want    string
	}{
		{name: "the session's own conversation", current: "", want: "s1"},
		{name: "after a rollover or an accepted plan", current: "rolled", want: "rolled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &convmocks.FakeConversationRepository{}
			repo.GetCurrentConversationIDReturns(tt.current)
			if got := liveThreadID(repo, "s1"); got != tt.want {
				t.Errorf("liveThreadID() = %q, want %q", got, tt.want)
			}
		})
	}
}

// serveDirect runs one direct task the way the serve loop does, on a thread a
// rollover moved from "first" to "rolled", and returns the frames it wrote.
func serveDirect(t *testing.T, tools *agentdomainmocks.FakeToolService, ctl *headlessControl, task string, fn sdk.ChatCompletionMessageToolCallFunction, parseErr error) string {
	t.Helper()
	repo := conversation.NewPersistentConversationRepository(nil, nil, storage.NewMemoryStorage())
	repo.SetConversationID("rolled")
	if ctl == nil {
		ctl = newHeadlessControl(&agentdomainmocks.FakeAgentService{}, &convmocks.FakeMessageQueue{}, "first")
	}
	req := agentdomain.AgentRequest{RequestID: "first", Model: "m"}
	return captureStdout(t, func() {
		_ = runServeDirect(t.Context(), tools, repo, agentdomain.AgentModePlan, ctl, req, task, fn, parseErr)
	})
}

func TestRunServeDirectReportsWhatCannotRunAsARun(t *testing.T) {
	readOnly := []sdk.ChatCompletionTool{{Function: sdk.FunctionObject{Name: "Read"}}}
	tests := []struct {
		name     string
		task     string
		fn       sdk.ChatCompletionMessageToolCallFunction
		parseErr error
		enabled  bool
		want     string
	}{
		{name: "a malformed tool call", task: "!!Read", parseErr: errors.New("invalid tool call"), want: "invalid tool call"},
		{name: "a disabled tool", task: `!!Nope(a="b")`, fn: sdk.ChatCompletionMessageToolCallFunction{Name: "Nope"}, want: "tool Nope is not enabled"},
		{name: "a tool the mode does not allow", task: `!!Write(file_path="x")`, fn: sdk.ChatCompletionMessageToolCallFunction{Name: "Write"}, enabled: true, want: "tool Write is not available"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := &agentdomainmocks.FakeToolService{}
			tools.IsToolEnabledReturns(tt.enabled)
			tools.ListToolsForModeReturns(readOnly)

			out := serveDirect(t, tools, nil, tt.task, tt.fn, tt.parseErr)

			if tools.ExecuteToolDirectCallCount() != 0 {
				t.Error("the tool ran")
			}
			for _, want := range []string{"RUN_STARTED", "RUN_ERROR", tt.want} {
				if !strings.Contains(out, want) {
					t.Errorf("the frames lack %q, and a client would wait for the run forever:\n%s", want, out)
				}
			}
		})
	}
}

func TestRunServeDirectRunsACommandInAnyModeOnTheLiveConversation(t *testing.T) {
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(true)
	tools.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{ToolName: "Bash", Success: true}, nil)

	out := serveDirect(t, tools, nil, "!ls", sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"ls"}`}, nil)

	if tools.ExecuteToolDirectCallCount() != 1 {
		t.Error("a !command did not run in plan mode")
	}
	if !strings.Contains(out, `"threadId":"rolled"`) || strings.Contains(out, `"threadId":"first"`) {
		t.Errorf("the run must name the live conversation, or the client reloads the old one:\n%s", out)
	}
}

func TestInterruptStopsTheDirectCommandTheServeLoopRuns(t *testing.T) {
	ctl := newHeadlessControl(&agentdomainmocks.FakeAgentService{}, &convmocks.FakeMessageQueue{}, "first")
	tools := &agentdomainmocks.FakeToolService{}
	tools.IsToolEnabledReturns(true)
	tools.ExecuteToolDirectStub = func(ctx context.Context, _ sdk.ChatCompletionMessageToolCallFunction) (*agentdomain.ToolExecutionResult, error) {
		ctl.dispatchLine([]byte(`{"type":"interrupt"}`))
		<-ctx.Done()
		return &agentdomain.ToolExecutionResult{ToolName: "Bash", Success: false, Error: "command was cancelled and killed"}, nil
	}

	out := serveDirect(t, tools, ctl, "!sleep 100", sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"sleep 100"}`}, nil)

	if !strings.Contains(out, `"outcome":{"type":"cancelled"}`) {
		t.Errorf("the run did not end as cancelled:\n%s", out)
	}
	if ctl.directCancel.Load() != nil {
		t.Error("a finished command left its cancel behind")
	}
	ctl.dispatchLine([]byte(`{"type":"interrupt"}`))
}
