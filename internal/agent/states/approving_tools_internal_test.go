package states

import (
	"context"
	"sync"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func TestApprovingToolsState_RejectionEntryKeepsArguments(t *testing.T) {
	var published []agentdomain.ChatEvent
	ctx := &StateContext{
		Request:          &agentdomain.AgentRequest{RequestID: "req-1"},
		PublishChatEvent: func(e agentdomain.ChatEvent) { published = append(published, e) },
	}
	s := &ApprovingToolsState{ctx: ctx}

	tc := sdk.ChatCompletionMessageToolCall{
		ID:       "call-0",
		Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"rm -rf /tmp/x"}`},
	}

	entry := s.buildRejectionEntry(tc, "")

	require.NotNil(t, entry.ToolExecution)
	assert.Equal(t, map[string]any{"command": "rm -rf /tmp/x"}, entry.ToolExecution.Arguments)

	require.Len(t, published, 1)
	progress, ok := published[0].(agentdomain.ToolExecutionProgressEvent)
	require.True(t, ok, "rejection must publish a ToolExecutionProgressEvent")
	assert.Equal(t, "call-0", progress.ToolCallID)
	assert.Equal(t, "failed", progress.Status)

	tc.Function.Arguments = "not-json"
	entry = s.buildRejectionEntry(tc, "")
	require.NotNil(t, entry.ToolExecution)
	assert.NotNil(t, entry.ToolExecution.Arguments, "malformed args must fall back to an empty map")
}

func TestApprovingToolsState_JudgeRejectionCarriesReason(t *testing.T) {
	var published []agentdomain.ChatEvent
	ctx := &StateContext{
		Request:          &agentdomain.AgentRequest{RequestID: "req-1"},
		PublishChatEvent: func(e agentdomain.ChatEvent) { published = append(published, e) },
	}
	s := &ApprovingToolsState{ctx: ctx}

	tc := sdk.ChatCompletionMessageToolCall{
		ID:       "call-0",
		Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"rm -rf /tmp/x"}`},
	}

	entry := s.buildRejectionEntry(tc, "judge unavailable: timeout")

	content, err := entry.Message.Content.AsMessageContent0()
	require.NoError(t, err)
	assert.Contains(t, content, "Tool execution rejected by judge: Bash")
	assert.Contains(t, content, "Rejection reason: judge unavailable: timeout")
	require.NotNil(t, entry.ToolExecution)
	assert.False(t, entry.ToolExecution.Rejected, "a judge rejection must not end the turn")
	assert.False(t, entry.ToolExecution.Success)
	assert.Equal(t, "rejected by judge: judge unavailable: timeout", entry.ToolExecution.Error)
}

// A call the approval policy does not gate (TodoWrite batched with a gated
// tool) must execute directly instead of prompting - issue #881 follow-up:
// only gated calls reach RequestToolApproval.
func TestApprovingToolsState_UngatedToolRunsWithoutPrompt(t *testing.T) {
	var mu sync.Mutex
	var approvalPrompts []string
	executed := make(map[string]bool)
	approvedByTool := make(map[string]bool)

	todo := sdk.ChatCompletionMessageToolCall{
		ID:       "call-0",
		Function: sdk.ChatCompletionMessageToolCallFunction{Name: "TodoWrite", Arguments: `{"todos":[]}`},
	}
	gated := sdk.ChatCompletionMessageToolCall{
		ID:       "call-1",
		Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Write", Arguments: `{}`},
	}
	calls := []*sdk.ChatCompletionMessageToolCall{&todo, &gated}
	needing := []sdk.ChatCompletionMessageToolCall{}
	results := []convdomain.ConversationEntry{}
	conversation := []sdk.Message{}
	index := 0
	var wg sync.WaitGroup
	var mtx sync.Mutex
	events := make(chan AgentEvent, 4)

	ctx := &StateContext{
		Request:               &agentdomain.AgentRequest{RequestID: "req-1", IsChatMode: true},
		AgentCtx:              &AgentContext{Ctx: context.Background(), Conversation: &conversation},
		Events:                events,
		WaitGroup:             &wg,
		Mutex:                 &mtx,
		CurrentToolCalls:      &calls,
		ToolsNeedingApproval:  &needing,
		CurrentToolIndex:      &index,
		ToolResults:           &results,
		MaxConcurrentTools:    2,
		ShouldRequireApproval: func(tc *sdk.ChatCompletionMessageToolCall, isChatMode bool) bool { return tc.Function.Name == "Write" },
		RequestToolApproval: func(tc sdk.ChatCompletionMessageToolCall) (bool, string, error) {
			mu.Lock()
			defer mu.Unlock()
			approvalPrompts = append(approvalPrompts, tc.Function.Name)
			return true, "", nil
		},
		ExecuteToolInternal: func(tc sdk.ChatCompletionMessageToolCall, isApproved bool) convdomain.ConversationEntry {
			mu.Lock()
			defer mu.Unlock()
			executed[tc.Function.Name] = true
			approvedByTool[tc.Function.Name] = isApproved
			return convdomain.ConversationEntry{
				Message: sdk.Message{Role: sdk.Tool, ToolCallID: &tc.ID},
				ToolExecution: &agentdomain.ToolExecutionResult{
					ToolName: tc.Function.Name,
					Success:  true,
				},
			}
		},
		AddMessage:         func(convdomain.ConversationEntry) error { return nil },
		PublishToolResults: func([]convdomain.ConversationEntry) {},
		GetAgentMode:       func() agentdomain.AgentMode { return agentdomain.AgentModeStandard },
	}
	s := &ApprovingToolsState{ctx: ctx}

	require.NoError(t, s.Handle(MessageReceivedEvent{}))

	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("approval round did not finish")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"Write"}, approvalPrompts, "only the gated tool prompts for approval")
	assert.True(t, executed["TodoWrite"], "ungated TodoWrite must execute")
	assert.True(t, executed["Write"], "gated Write must execute after approval")
	assert.False(t, approvedByTool["TodoWrite"], "TodoWrite runs without the approved flag")
	assert.True(t, approvedByTool["Write"], "Write runs with the approved flag")

	require.Len(t, *ctx.ToolResults, 2)
	assert.Equal(t, []string{"TodoWrite", "Write"},
		[]string{(*ctx.ToolResults)[0].ToolExecution.ToolName, (*ctx.ToolResults)[1].ToolExecution.ToolName},
		"results keep tool-call order")
}
