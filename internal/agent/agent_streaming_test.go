package agent

import (
	"context"
	"testing"

	assert "github.com/stretchr/testify/assert"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestBuildAssistantMessage(t *testing.T) {
	toolCall := &sdk.ChatCompletionMessageToolCall{
		ID:   "call_1",
		Type: sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Bash",
			Arguments: `{"command":"ls"}`,
		},
	}

	tests := []struct {
		name             string
		content          sdk.MessageContent
		reasoning        string
		toolCalls        []*sdk.ChatCompletionMessageToolCall
		wantReasoning    bool
		wantToolCalls    bool
		wantToolCallsLen int
	}{
		{
			name:          "reasoning without tool calls is preserved",
			content:       sdk.NewMessageContent("Let me try a different approach."),
			reasoning:     "the previous command failed because the path was wrong",
			toolCalls:     nil,
			wantReasoning: true,
			wantToolCalls: false,
		},
		{
			name:             "reasoning with tool calls is preserved",
			content:          sdk.NewMessageContent(""),
			reasoning:        "I need to list the directory first",
			toolCalls:        []*sdk.ChatCompletionMessageToolCall{toolCall},
			wantReasoning:    true,
			wantToolCalls:    true,
			wantToolCallsLen: 1,
		},
		{
			name:          "empty reasoning yields nil pointers",
			content:       sdk.NewMessageContent("hello"),
			reasoning:     "",
			toolCalls:     nil,
			wantReasoning: false,
			wantToolCalls: false,
		},
		{
			name:             "tool calls without reasoning",
			content:          sdk.NewMessageContent(""),
			reasoning:        "",
			toolCalls:        []*sdk.ChatCompletionMessageToolCall{toolCall},
			wantReasoning:    false,
			wantToolCalls:    true,
			wantToolCallsLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := buildAssistantMessage(tt.content, tt.reasoning, tt.toolCalls)

			assert.Equal(t, sdk.Assistant, msg.Role)

			if tt.wantReasoning {
				if assert.NotNil(t, msg.Reasoning, "Reasoning should be set") {
					assert.Equal(t, tt.reasoning, *msg.Reasoning)
				}
				if assert.NotNil(t, msg.ReasoningContent, "ReasoningContent should be set") {
					assert.Equal(t, tt.reasoning, *msg.ReasoningContent)
				}
			} else {
				assert.Nil(t, msg.Reasoning, "Reasoning should be nil when reasoning is empty")
				assert.Nil(t, msg.ReasoningContent, "ReasoningContent should be nil when reasoning is empty")
			}

			if tt.wantToolCalls {
				if assert.NotNil(t, msg.ToolCalls) {
					assert.Equal(t, tt.wantToolCallsLen, len(*msg.ToolCalls))
				}
			} else {
				assert.Nil(t, msg.ToolCalls)
			}
		})
	}
}

// TestPersistPartialAssistantMessage_KeepsContent verifies that a half-streamed
// LLM response (text + reasoning) is appended to the conversation and the
// repo when the user cancels mid-generation - so a partially-written poem
// isn't lost the moment Esc is pressed.
func TestPersistPartialAssistantMessage_KeepsContent(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.AddMessageReturns(nil)

	conversation := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("Write a long poem")},
	}

	agent := &EventDrivenAgent{
		service:  &AgentServiceImpl{conversationRepo: repo},
		agentCtx: &domain.AgentContext{Conversation: &conversation, Ctx: context.Background()},
		req:      &domain.AgentRequest{RequestID: "r1", Model: "deepseek/deepseek-v4-flash"},
	}

	partial := sdk.Message{
		Role:    sdk.Assistant,
		Content: sdk.NewMessageContent("Roses are red,\nViolets are blue,\nGo concurrency..."),
	}

	agent.persistPartialAssistantMessage(partial)

	assert.Len(t, conversation, 2, "partial assistant message should be appended")
	assert.Equal(t, sdk.Assistant, conversation[1].Role)
	got, _ := conversation[1].Content.AsMessageContent0()
	assert.Contains(t, got, "Roses are red")
	assert.Equal(t, 1, repo.AddMessageCallCount(), "partial assistant message should be saved to repo")
}

// TestPersistPartialAssistantMessage_SkipsEmpty verifies that a cancel before
// the model emits anything does NOT add an empty assistant message to history.
func TestPersistPartialAssistantMessage_SkipsEmpty(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	conversation := []sdk.Message{}

	agent := &EventDrivenAgent{
		service:  &AgentServiceImpl{conversationRepo: repo},
		agentCtx: &domain.AgentContext{Conversation: &conversation, Ctx: context.Background()},
		req:      &domain.AgentRequest{RequestID: "r1"},
	}

	agent.persistPartialAssistantMessage(sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("")})

	assert.Empty(t, conversation, "empty partial should not be appended")
	assert.Equal(t, 0, repo.AddMessageCallCount())
}

func TestOutboundConversation_AppendsTailWithoutMutatingShared(t *testing.T) {
	conv := []sdk.Message{
		{Role: sdk.System, Content: sdk.NewMessageContent("system")},
		{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
	}
	tail := sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("<system-reminder>\nCurrent date: today\n</system-reminder>")}

	a := &EventDrivenAgent{
		agentCtx:     &domain.AgentContext{Conversation: &conv},
		volatileTail: []sdk.Message{tail},
	}

	out := a.outboundConversation()

	assert.Len(t, out, 3)
	assert.Equal(t, tail, out[2])
	assert.Len(t, conv, 2, "shared conversation must not grow the ephemeral tail")
}

func TestOutboundConversation_NoTailReturnsSharedAsIs(t *testing.T) {
	conv := []sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("hi")}}
	a := &EventDrivenAgent{agentCtx: &domain.AgentContext{Conversation: &conv}}

	assert.Equal(t, conv, a.outboundConversation())
}

func TestOutboundConversation_SkipsTailWhileAwaitingToolResults(t *testing.T) {
	toolCalls := []sdk.ChatCompletionMessageToolCall{{ID: "call_1"}}
	conv := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
		{Role: sdk.Assistant, ToolCalls: &toolCalls},
	}
	tail := sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("<system-reminder>\nCurrent date: today\n</system-reminder>")}
	a := &EventDrivenAgent{
		agentCtx:     &domain.AgentContext{Conversation: &conv},
		volatileTail: []sdk.Message{tail},
	}

	assert.Equal(t, conv, a.outboundConversation(), "a trailing user tail would orphan the open tool_calls")
}
