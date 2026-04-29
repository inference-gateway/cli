package agent

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	sdk "github.com/inference-gateway/sdk"
)

func TestBuildAssistantMessage(t *testing.T) {
	toolCall := &sdk.ChatCompletionMessageToolCall{
		Id:   "call_1",
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
