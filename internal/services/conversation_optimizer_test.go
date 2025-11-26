package services_test

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
	services "github.com/inference-gateway/cli/internal/services"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

// TestConversationOptimizer_ToolCallIntegrity_BufferBoundary tests tool calls at buffer boundaries
func TestConversationOptimizer_ToolCallIntegrity_BufferBoundary(t *testing.T) {
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("First request")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 1")},
		{Role: "user", Content: sdk.NewMessageContent("Second request")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 2")},
		{
			Role:    "assistant",
			Content: sdk.NewMessageContent("Let me use some tools"),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool1"}},
				{Id: "call_2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool2"}},
			},
		},
		{Role: "tool", Content: sdk.NewMessageContent("result 1"), ToolCallId: stringPtr("call_1")},
		{Role: "tool", Content: sdk.NewMessageContent("result 2"), ToolCallId: stringPtr("call_2")},
		{Role: "assistant", Content: sdk.NewMessageContent("Done")},
	}

	optimizer := createTestOptimizer(2)
	result := optimizer.OptimizeMessages(messages, false)

	validateAssistantToolCallsPreserved(t, result)
	validateNoOrphanedToolCalls(t, result)
}

// TestConversationOptimizer_ToolCallIntegrity_ToolResponseInBuffer tests tool responses pulling in assistant
func TestConversationOptimizer_ToolCallIntegrity_ToolResponseInBuffer(t *testing.T) {
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("First request")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 1")},
		{Role: "user", Content: sdk.NewMessageContent("Second request")},
		{
			Role:    "assistant",
			Content: sdk.NewMessageContent("Using tools"),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "bash"}},
			},
		},
		{Role: "tool", Content: sdk.NewMessageContent("bash output"), ToolCallId: stringPtr("call_1")},
		{Role: "assistant", Content: sdk.NewMessageContent("Analysis of output")},
	}

	optimizer := createTestOptimizer(2)
	result := optimizer.OptimizeMessages(messages, false)

	validateToolResponseHasAssistant(t, result)
	validateNoOrphanedToolCalls(t, result)
}

// TestConversationOptimizer_ToolCallIntegrity_MultipleGroups tests multiple tool call groups
func TestConversationOptimizer_ToolCallIntegrity_MultipleGroups(t *testing.T) {
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("Request 1")},
		{
			Role:    "assistant",
			Content: sdk.NewMessageContent("Using tools group 1"),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "read"}},
			},
		},
		{Role: "tool", Content: sdk.NewMessageContent("file content"), ToolCallId: stringPtr("call_1")},
		{Role: "user", Content: sdk.NewMessageContent("Request 2")},
		{
			Role:    "assistant",
			Content: sdk.NewMessageContent("Using tools group 2"),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "write"}},
				{Id: "call_3", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "bash"}},
			},
		},
		{Role: "tool", Content: sdk.NewMessageContent("write success"), ToolCallId: stringPtr("call_2")},
		{Role: "tool", Content: sdk.NewMessageContent("bash output"), ToolCallId: stringPtr("call_3")},
		{Role: "assistant", Content: sdk.NewMessageContent("All done")},
	}

	optimizer := createTestOptimizer(3)
	result := optimizer.OptimizeMessages(messages, false)

	validateNoOrphanedToolCalls(t, result)
}

// TestConversationOptimizer_ToolCallIntegrity_ExactBufferStart tests tool calls at exact buffer start
func TestConversationOptimizer_ToolCallIntegrity_ExactBufferStart(t *testing.T) {
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("Request 1")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 1")},
		{Role: "user", Content: sdk.NewMessageContent("Request 2")},
		{
			Role:    "assistant",
			Content: sdk.NewMessageContent("Using tools"),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "grep"}},
			},
		},
		{Role: "tool", Content: sdk.NewMessageContent("search results"), ToolCallId: stringPtr("call_1")},
	}

	optimizer := createTestOptimizer(2)
	result := optimizer.OptimizeMessages(messages, false)

	validateNoOrphanedToolCalls(t, result)
}

// TestConversationOptimizer_ToolCallIntegrity_NoToolCalls tests normal optimization without tool calls
func TestConversationOptimizer_ToolCallIntegrity_NoToolCalls(t *testing.T) {
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("Request 1")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 1")},
		{Role: "user", Content: sdk.NewMessageContent("Request 2")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 2")},
		{Role: "user", Content: sdk.NewMessageContent("Request 3")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 3")},
		{Role: "user", Content: sdk.NewMessageContent("Request 4")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 4")},
	}

	optimizer := createTestOptimizer(2)
	result := optimizer.OptimizeMessages(messages, false)

	validateNoOrphanedToolCalls(t, result)
	assert.NotEmpty(t, result)
}

// TestConversationOptimizer_ToolCallIntegrity_PartialResponses tests incomplete tool call groups
func TestConversationOptimizer_ToolCallIntegrity_PartialResponses(t *testing.T) {
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("Request")},
		{Role: "assistant", Content: sdk.NewMessageContent("Old response")},
		{
			Role:    "assistant",
			Content: sdk.NewMessageContent("Calling multiple tools"),
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool1"}},
				{Id: "call_2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool2"}},
				{Id: "call_3", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool3"}},
			},
		},
		{Role: "tool", Content: sdk.NewMessageContent("result 1"), ToolCallId: stringPtr("call_1")},
		{Role: "tool", Content: sdk.NewMessageContent("result 2"), ToolCallId: stringPtr("call_2")},
		{Role: "tool", Content: sdk.NewMessageContent("result 3"), ToolCallId: stringPtr("call_3")},
	}

	optimizer := createTestOptimizer(1)
	result := optimizer.OptimizeMessages(messages, false)

	validateNoOrphanedToolCalls(t, result)
}

// TestConversationOptimizer_EdgeCases tests edge cases and boundary conditions
func TestConversationOptimizer_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		messages    []sdk.Message
		bufferSize  int
		expectEmpty bool
		description string
	}{
		{
			name:        "empty conversation",
			messages:    []sdk.Message{},
			bufferSize:  2,
			expectEmpty: true,
			description: "Empty input should return empty output",
		},
		{
			name: "single message",
			messages: []sdk.Message{
				{Role: "user", Content: sdk.NewMessageContent("Hello")},
			},
			bufferSize:  2,
			expectEmpty: false,
			description: "Single message should be preserved",
		},
		{
			name: "only system messages",
			messages: []sdk.Message{
				{Role: "system", Content: sdk.NewMessageContent("System prompt 1")},
				{Role: "system", Content: sdk.NewMessageContent("System prompt 2")},
			},
			bufferSize:  2,
			expectEmpty: false,
			description: "System messages should be preserved",
		},
		{
			name: "very short conversation",
			messages: []sdk.Message{
				{Role: "user", Content: sdk.NewMessageContent("Hi")},
				{Role: "assistant", Content: sdk.NewMessageContent("Hello")},
			},
			bufferSize:  2,
			expectEmpty: false,
			description: "Short conversations should not be optimized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			optimizer := services.NewConversationOptimizer(services.OptimizerConfig{
				Enabled:    true,
				AutoAt:     80,
				BufferSize: tt.bufferSize,
				Client:     nil,
				Config:     &config.Config{},
				Tokenizer:  nil,
			})

			result := optimizer.OptimizeMessages(tt.messages, false)

			if tt.expectEmpty {
				assert.Empty(t, result)
			} else {
				assert.NotEmpty(t, result)
			}
		})
	}
}

// TestConversationOptimizer_DisabledOptimization tests that optimization can be disabled
func TestConversationOptimizer_DisabledOptimization(t *testing.T) {
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("Request 1")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 1")},
		{Role: "user", Content: sdk.NewMessageContent("Request 2")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 2")},
		{Role: "user", Content: sdk.NewMessageContent("Request 3")},
		{Role: "assistant", Content: sdk.NewMessageContent("Response 3")},
	}

	optimizer := services.NewConversationOptimizer(services.OptimizerConfig{
		Enabled:    false,
		AutoAt:     80,
		BufferSize: 2,
		Client:     nil,
		Config:     &config.Config{},
		Tokenizer:  nil,
	})

	result := optimizer.OptimizeMessages(messages, false)

	assert.Equal(t, len(messages), len(result))
	assert.Equal(t, messages, result)
}

// Helper functions

func stringPtr(s string) *string {
	return &s
}

// createTestOptimizer creates an optimizer with the given buffer size for testing
func createTestOptimizer(bufferSize int) *services.ConversationOptimizer {
	return services.NewConversationOptimizer(services.OptimizerConfig{
		Enabled:    true,
		AutoAt:     80,
		BufferSize: bufferSize,
		Client:     nil,
		Config:     &config.Config{},
		Tokenizer:  nil,
	})
}

// validateAssistantToolCallsPreserved checks that assistant with tool calls has all responses
func validateAssistantToolCallsPreserved(t *testing.T, result []sdk.Message) {
	t.Helper()

	var assistantIdx = -1
	for i, msg := range result {
		if msg.Role == "assistant" && msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
			assistantIdx = i
			break
		}
	}
	require.NotEqual(t, -1, assistantIdx, "Assistant message with tool calls should be preserved")

	toolCalls := *result[assistantIdx].ToolCalls
	expectedToolCallIDs := make(map[string]bool)
	for _, tc := range toolCalls {
		expectedToolCallIDs[tc.Id] = true
	}

	for i := assistantIdx + 1; i < len(result); i++ {
		if result[i].Role == "tool" && result[i].ToolCallId != nil {
			delete(expectedToolCallIDs, *result[i].ToolCallId)
		} else if result[i].Role == "assistant" || result[i].Role == "user" {
			break
		}
	}

	assert.Empty(t, expectedToolCallIDs, "All tool call IDs should have corresponding responses")
}

// validateToolResponseHasAssistant checks that tool response has preceding assistant message
func validateToolResponseHasAssistant(t *testing.T, result []sdk.Message) {
	t.Helper()

	var toolIdx = -1
	for i, msg := range result {
		if msg.Role == "tool" {
			toolIdx = i
			break
		}
	}
	require.NotEqual(t, -1, toolIdx, "Tool response should be preserved")

	toolCallId := *result[toolIdx].ToolCallId
	found := false
	for i := toolIdx - 1; i >= 0; i-- {
		if result[i].Role == "assistant" && result[i].ToolCalls != nil {
			for _, tc := range *result[i].ToolCalls {
				if tc.Id == toolCallId {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
	}
	assert.True(t, found, "Assistant message with matching tool call ID should precede tool response")
}

// validateNoOrphanedToolCalls checks that every assistant message with tool calls
// has all its corresponding tool responses, and every tool response has a corresponding
// assistant message with tool calls
func validateNoOrphanedToolCalls(t *testing.T, messages []sdk.Message) {
	t.Helper()

	expectedToolCallIDs, toolResponseIDs := collectToolCallIDs(messages)
	validateToolCallsHaveResponses(t, expectedToolCallIDs, toolResponseIDs)
	validateToolResponsesHaveToolCalls(t, toolResponseIDs, expectedToolCallIDs)
	validateImmediateToolResponses(t, messages)
}

// collectToolCallIDs gathers all tool call IDs and tool response IDs from messages
func collectToolCallIDs(messages []sdk.Message) (map[string]int, map[string]int) {
	expectedToolCallIDs := make(map[string]int)
	toolResponseIDs := make(map[string]int)

	for i, msg := range messages {
		if msg.Role == "assistant" && msg.ToolCalls != nil {
			for _, tc := range *msg.ToolCalls {
				if tc.Id != "" {
					expectedToolCallIDs[tc.Id] = i
				}
			}
		} else if msg.Role == "tool" && msg.ToolCallId != nil {
			toolResponseIDs[*msg.ToolCallId] = i
		}
	}

	return expectedToolCallIDs, toolResponseIDs
}

// validateToolCallsHaveResponses ensures all tool calls have corresponding responses
func validateToolCallsHaveResponses(t *testing.T, expectedToolCallIDs, toolResponseIDs map[string]int) {
	t.Helper()

	for toolCallID, assistantIdx := range expectedToolCallIDs {
		responseIdx, hasResponse := toolResponseIDs[toolCallID]
		assert.True(t, hasResponse,
			"Tool call ID %s from assistant at index %d has no corresponding tool response",
			toolCallID, assistantIdx)
		if hasResponse {
			assert.Greater(t, responseIdx, assistantIdx,
				"Tool response at index %d should come after assistant at index %d",
				responseIdx, assistantIdx)
		}
	}
}

// validateToolResponsesHaveToolCalls ensures all tool responses have corresponding tool calls
func validateToolResponsesHaveToolCalls(t *testing.T, toolResponseIDs, expectedToolCallIDs map[string]int) {
	t.Helper()

	for toolCallID, toolIdx := range toolResponseIDs {
		assistantIdx, hasToolCall := expectedToolCallIDs[toolCallID]
		assert.True(t, hasToolCall,
			"Tool response with ID %s at index %d has no corresponding assistant tool call",
			toolCallID, toolIdx)
		if hasToolCall {
			assert.Less(t, assistantIdx, toolIdx,
				"Assistant with tool call at index %d should come before tool response at index %d",
				assistantIdx, toolIdx)
		}
	}
}

// validateImmediateToolResponses checks that tool responses immediately follow their assistant messages
func validateImmediateToolResponses(t *testing.T, messages []sdk.Message) {
	t.Helper()

	for i, msg := range messages {
		if msg.Role == "assistant" && msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
			validateToolResponsesForAssistant(t, messages, i, *msg.ToolCalls)
		}
	}
}

// validateToolResponsesForAssistant validates responses for a specific assistant message
func validateToolResponsesForAssistant(t *testing.T, messages []sdk.Message, assistantIdx int, toolCalls []sdk.ChatCompletionMessageToolCall) {
	t.Helper()

	foundResponses := make(map[string]bool)

	for j := assistantIdx + 1; j < len(messages); j++ {
		if shouldStopSearching(messages[j]) {
			break
		}

		if messages[j].Role == "tool" && messages[j].ToolCallId != nil {
			processToolResponse(t, messages, assistantIdx, j, toolCalls, foundResponses)
		}
	}

	verifyAllToolCallsHaveResponses(t, toolCalls, foundResponses, assistantIdx)
}

// shouldStopSearching determines if we should stop searching for tool responses
func shouldStopSearching(msg sdk.Message) bool {
	return msg.Role == "assistant" || msg.Role == "user"
}

// processToolResponse validates a tool response against expected tool calls
func processToolResponse(t *testing.T, messages []sdk.Message, assistantIdx, toolResponseIdx int,
	toolCalls []sdk.ChatCompletionMessageToolCall, foundResponses map[string]bool) {
	t.Helper()

	toolCallID := *messages[toolResponseIdx].ToolCallId
	matchesOurCall := false

	for _, tc := range toolCalls {
		if tc.Id == toolCallID {
			matchesOurCall = true
			foundResponses[tc.Id] = true
			break
		}
	}

	if !matchesOurCall && !hasIntermediateAssistant(messages, assistantIdx, toolResponseIdx) {
		t.Errorf("Tool response at index %d with call_id %s doesn't match assistant at index %d",
			toolResponseIdx, toolCallID, assistantIdx)
	}
}

// hasIntermediateAssistant checks if there's an assistant message between two indices
func hasIntermediateAssistant(messages []sdk.Message, start, end int) bool {
	for k := start + 1; k < end; k++ {
		if messages[k].Role == "assistant" {
			return true
		}
	}
	return false
}

// verifyAllToolCallsHaveResponses ensures all tool calls received responses
func verifyAllToolCallsHaveResponses(t *testing.T, toolCalls []sdk.ChatCompletionMessageToolCall,
	foundResponses map[string]bool, assistantIdx int) {
	t.Helper()

	for _, tc := range toolCalls {
		if tc.Id != "" {
			assert.True(t, foundResponses[tc.Id],
				"Tool call %s from assistant at index %d has no response in the immediate following messages",
				tc.Id, assistantIdx)
		}
	}
}
