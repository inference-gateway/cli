package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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
	assert.Contains(t, content, "Tool execution rejected by user: Bash")
	assert.Contains(t, content, "Rejection reason: judge unavailable: timeout")
	require.NotNil(t, entry.ToolExecution)
	assert.True(t, entry.ToolExecution.Rejected)
}
