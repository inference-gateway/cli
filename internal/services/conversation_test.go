package services

import (
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
)

func TestInMemoryConversationRepository_RemovePendingToolCallByID(t *testing.T) {
	tests := []struct {
		name               string
		setupToolCalls     []sdk.ChatCompletionMessageToolCall
		removeToolCallID   string
		expectedRemaining  int
		expectedRemovedIDs []string
	}{
		{
			name: "remove_existing_tool_call",
			setupToolCalls: []sdk.ChatCompletionMessageToolCall{
				{
					Id: "tool-call-1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "test_function",
						Arguments: `{"arg": "value"}`,
					},
				},
			},
			removeToolCallID:   "tool-call-1",
			expectedRemaining:  0,
			expectedRemovedIDs: []string{"tool-call-1"},
		},
		{
			name: "remove_non_existent_tool_call",
			setupToolCalls: []sdk.ChatCompletionMessageToolCall{
				{
					Id: "tool-call-1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "test_function",
						Arguments: `{"arg": "value"}`,
					},
				},
			},
			removeToolCallID:   "non-existent-id",
			expectedRemaining:  1,
			expectedRemovedIDs: []string{},
		},
		{
			name: "remove_from_multiple_tool_calls",
			setupToolCalls: []sdk.ChatCompletionMessageToolCall{
				{
					Id: "tool-call-1",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "function_1",
						Arguments: `{"arg1": "value1"}`,
					},
				},
				{
					Id: "tool-call-2",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "function_2",
						Arguments: `{"arg2": "value2"}`,
					},
				},
				{
					Id: "tool-call-3",
					Function: sdk.ChatCompletionMessageToolCallFunction{
						Name:      "function_3",
						Arguments: `{"arg3": "value3"}`,
					},
				},
			},
			removeToolCallID:   "tool-call-2",
			expectedRemaining:  2,
			expectedRemovedIDs: []string{"tool-call-2"},
		},
		{
			name:               "remove_from_empty_repository",
			setupToolCalls:     []sdk.ChatCompletionMessageToolCall{},
			removeToolCallID:   "any-id",
			expectedRemaining:  0,
			expectedRemovedIDs: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewInMemoryConversationRepository(nil, nil)

			for _, toolCall := range tt.setupToolCalls {
				err := repo.AddPendingToolCall(toolCall, make(chan domain.ApprovalAction))
				assert.NoError(t, err)
			}

			initialMessages := repo.GetMessages()
			assert.Equal(t, len(tt.setupToolCalls), len(initialMessages))

			repo.RemovePendingToolCallByID(tt.removeToolCallID)

			finalMessages := repo.GetMessages()
			assert.Equal(t, tt.expectedRemaining, len(finalMessages))

			for _, removedID := range tt.expectedRemovedIDs {
				found := false
				for _, msg := range finalMessages {
					if msg.PendingToolCall != nil && msg.PendingToolCall.Id == removedID {
						found = true
						break
					}
				}
				assert.False(t, found, "Tool call with ID %s should have been removed but was found", removedID)
			}

			remainingIDs := make(map[string]bool)
			for _, msg := range finalMessages {
				if msg.PendingToolCall != nil {
					remainingIDs[msg.PendingToolCall.Id] = true
				}
			}

			for _, msg := range finalMessages {
				if msg.PendingToolCall != nil {
					originalFound := false
					for _, originalToolCall := range tt.setupToolCalls {
						if originalToolCall.Id == msg.PendingToolCall.Id {
							originalFound = true
							break
						}
					}
					assert.True(t, originalFound, "Tool call with ID %s should be from original setup", msg.PendingToolCall.Id)
				}
			}
		})
	}
}
