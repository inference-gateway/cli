package services_test

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
	services "github.com/inference-gateway/cli/internal/services"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

// testCase represents a single test case for OptimizeMessages
type testCase struct {
	name              string
	messages          []sdk.Message
	keepFirstMessages int
	expectedValid     bool
	description       string
}

// getBasicToolCallTestCases returns basic tool call test cases
func getBasicToolCallTestCases() []testCase {
	return []testCase{
		{
			name: "unresolved tool calls with intermediate assistant message",
			messages: []sdk.Message{
				{Role: "user", Content: sdk.NewMessageContent("Hello")},
				{
					Role:    "assistant",
					Content: sdk.NewMessageContent("Let me use two tools"),
					ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
						{Id: "call_A", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "read"}},
						{Id: "call_B", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "write"}},
					},
				},
				{Role: "tool", Content: sdk.NewMessageContent("file content"), ToolCallId: stringPtr("call_A")},
				{Role: "assistant", Content: sdk.NewMessageContent("Intermediate response")}, // Breaks the loop
				{Role: "tool", Content: sdk.NewMessageContent("write success"), ToolCallId: stringPtr("call_B")},
				{Role: "assistant", Content: sdk.NewMessageContent("All done")},
				{Role: "user", Content: sdk.NewMessageContent("Thank you")},
			},
			keepFirstMessages: 2,
			expectedValid:     true,
			description:       "Should exclude assistant with incomplete tool calls when responses are interrupted",
		},
		{
			name: "all tool responses present",
			messages: []sdk.Message{
				{Role: "user", Content: sdk.NewMessageContent("Hello")},
				{Role: "assistant", Content: sdk.NewMessageContent("Response 1")},
				{Role: "user", Content: sdk.NewMessageContent("Request 2")},
				{
					Role:    "assistant",
					Content: sdk.NewMessageContent("Let me use two tools"),
					ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
						{Id: "call_A", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "read"}},
						{Id: "call_B", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "write"}},
					},
				},
				{Role: "tool", Content: sdk.NewMessageContent("file content"), ToolCallId: stringPtr("call_A")},
				{Role: "tool", Content: sdk.NewMessageContent("write success"), ToolCallId: stringPtr("call_B")},
				{Role: "assistant", Content: sdk.NewMessageContent("All done")},
				{Role: "user", Content: sdk.NewMessageContent("Thank you")},
			},
			keepFirstMessages: 2,
			expectedValid:     true,
			description:       "Should include all tool responses when they're complete and boundary is adjusted forward",
		},
		{
			name: "multiple tool call groups",
			messages: []sdk.Message{
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
			},
			keepFirstMessages: 3,
			expectedValid:     true,
			description:       "Should handle multiple tool call groups correctly",
		},
		{
			name: "no tool calls",
			messages: []sdk.Message{
				{Role: "user", Content: sdk.NewMessageContent("Request 1")},
				{Role: "assistant", Content: sdk.NewMessageContent("Response 1")},
				{Role: "user", Content: sdk.NewMessageContent("Request 2")},
				{Role: "assistant", Content: sdk.NewMessageContent("Response 2")},
				{Role: "user", Content: sdk.NewMessageContent("Request 3")},
				{Role: "assistant", Content: sdk.NewMessageContent("Response 3")},
			},
			keepFirstMessages: 2,
			expectedValid:     true,
			description:       "Should handle conversations without tool calls",
		},
		{
			name: "single tool call at boundary",
			messages: []sdk.Message{
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
			},
			keepFirstMessages: 2,
			expectedValid:     true,
			description:       "Should handle tool calls exactly at buffer boundary",
		},
		{
			name: "partial tool responses before user message",
			messages: []sdk.Message{
				{Role: "user", Content: sdk.NewMessageContent("Request")},
				{
					Role:    "assistant",
					Content: sdk.NewMessageContent("Calling multiple tools"),
					ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
						{Id: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool1"}},
						{Id: "call_2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool2"}},
					},
				},
				{Role: "tool", Content: sdk.NewMessageContent("result 1"), ToolCallId: stringPtr("call_1")},
				{Role: "user", Content: sdk.NewMessageContent("Next request")}, // User interrupts
				{Role: "tool", Content: sdk.NewMessageContent("result 2"), ToolCallId: stringPtr("call_2")},
			},
			keepFirstMessages: 1,
			expectedValid:     true,
			description:       "Should exclude assistant with incomplete tool calls when user interrupts",
		},
	}
}

// getRealConversationTestCase returns the real conversation test case
func getRealConversationTestCase() testCase {
	return testCase{
		name: "real conversation structure - fb27566d",
		messages: []sdk.Message{
			{Role: "user", Content: sdk.NewMessageContent("Request 1")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 1"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool1"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallId: stringPtr("call_00_1")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 2"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool2"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallId: stringPtr("call_00_2")},
			{Role: "user", Content: sdk.NewMessageContent("Request 2")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 3"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_3", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool3"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallId: stringPtr("call_00_3")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 4"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_4", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool4"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallId: stringPtr("call_00_4")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 5"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_5", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool5"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallId: stringPtr("call_00_5")},
			{Role: "user", Content: sdk.NewMessageContent("Request 3")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 6"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_6", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool6"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallId: stringPtr("call_00_6")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 7"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_7", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool7"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallId: stringPtr("call_00_7")},
			{Role: "user", Content: sdk.NewMessageContent("Request 4")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 8"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_8", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool8"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallId: stringPtr("call_00_8")},
			{Role: "assistant", Content: sdk.NewMessageContent("Let me use two tools"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_mw22yDQOyJlZQaFT2mmXnyBZ", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "read"}},
				{Id: "call_01_C9gJA1FoL22xfCrTHUWR2KYG", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "write"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("read result"), ToolCallId: stringPtr("call_00_mw22yDQOyJlZQaFT2mmXnyBZ")},
			{Role: "tool", Content: sdk.NewMessageContent("write result"), ToolCallId: stringPtr("call_01_C9gJA1FoL22xfCrTHUWR2KYG")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 9"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{Id: "call_00_9", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool9"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallId: stringPtr("call_00_9")},
			{Role: "user", Content: sdk.NewMessageContent("Final request")},
		},
		keepFirstMessages: 2,
		expectedValid:     true,
		description:       "Should handle real conversation with many sequential tool calls",
	}
}

// getToolCallIntegrityTestCases returns all tool call integrity test cases
func getToolCallIntegrityTestCases() []testCase {
	cases := getBasicToolCallTestCases()
	cases = append(cases, getRealConversationTestCase())
	return cases
}

// validateOptimizedResult validates that optimized messages have no incomplete tool calls
func validateOptimizedResult(t *testing.T, result []sdk.Message, description string) {
	t.Helper()

	validateNoOrphanedToolCalls(t, result)

	for i, msg := range result {
		if msg.Role != "assistant" || msg.ToolCalls == nil || len(*msg.ToolCalls) == 0 {
			continue
		}

		toolCallIDs := collectToolCallIDsFromMessage(msg)
		removeMatchingToolResponses(result, i, toolCallIDs)

		assert.Empty(t, toolCallIDs,
			"Assistant at index %d should have all tool responses or be excluded: %s",
			i, description)
	}
}

// collectToolCallIDsFromMessage extracts tool call IDs from an assistant message
func collectToolCallIDsFromMessage(msg sdk.Message) map[string]bool {
	toolCallIDs := make(map[string]bool)
	for _, tc := range *msg.ToolCalls {
		toolCallIDs[tc.Id] = true
	}
	return toolCallIDs
}

// removeMatchingToolResponses removes tool call IDs that have responses
func removeMatchingToolResponses(result []sdk.Message, assistantIdx int, toolCallIDs map[string]bool) {
	for j := assistantIdx + 1; j < len(result); j++ {
		if result[j].Role == "tool" && result[j].ToolCallId != nil {
			delete(toolCallIDs, *result[j].ToolCallId)
		} else if result[j].Role == "assistant" || result[j].Role == "user" {
			break
		}
	}
}

// TestOptimizeMessages_ToolCallIntegrity uses table-driven tests to verify
// that OptimizeMessages correctly handles tool calls in various scenarios
func TestOptimizeMessages_ToolCallIntegrity(t *testing.T) {
	tests := getToolCallIntegrityTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := createMockSDKClient(t, "Summary of conversation")

			optimizer := services.NewConversationOptimizer(services.OptimizerConfig{
				Enabled:           true,
				AutoAt:            80,
				BufferSize:        2,
				KeepFirstMessages: tt.keepFirstMessages,
				Client:            mockClient,
				Config:            &config.Config{},
				Tokenizer:         nil,
			})

			result := optimizer.OptimizeMessages(tt.messages, "deepseek/deepseek-chat", true)

			if tt.expectedValid {
				validateOptimizedResult(t, result, tt.description)
			}
		})
	}
}

// TestOptimizeMessages_EdgeCases tests edge cases and boundary conditions
func TestOptimizeMessages_EdgeCases(t *testing.T) {
	tests := []struct {
		name              string
		messages          []sdk.Message
		keepFirstMessages int
		force             bool
		expectEmpty       bool
		description       string
	}{
		{
			name:              "empty conversation",
			messages:          []sdk.Message{},
			keepFirstMessages: 2,
			force:             true,
			expectEmpty:       true,
			description:       "Empty input should return empty output",
		},
		{
			name: "single message",
			messages: []sdk.Message{
				{Role: "user", Content: sdk.NewMessageContent("Hello")},
			},
			keepFirstMessages: 2,
			force:             true,
			expectEmpty:       false,
			description:       "Single message should be preserved",
		},
		{
			name: "only system messages",
			messages: []sdk.Message{
				{Role: "system", Content: sdk.NewMessageContent("System prompt 1")},
				{Role: "system", Content: sdk.NewMessageContent("System prompt 2")},
			},
			keepFirstMessages: 2,
			force:             true,
			expectEmpty:       false,
			description:       "System messages should be preserved",
		},
		{
			name: "very short conversation - no optimization",
			messages: []sdk.Message{
				{Role: "user", Content: sdk.NewMessageContent("Hi")},
				{Role: "assistant", Content: sdk.NewMessageContent("Hello")},
			},
			keepFirstMessages: 2,
			force:             true,
			expectEmpty:       false,
			description:       "Short conversations should not be optimized",
		},
		{
			name: "optimization disabled",
			messages: []sdk.Message{
				{Role: "user", Content: sdk.NewMessageContent("Request 1")},
				{Role: "assistant", Content: sdk.NewMessageContent("Response 1")},
				{Role: "user", Content: sdk.NewMessageContent("Request 2")},
				{Role: "assistant", Content: sdk.NewMessageContent("Response 2")},
			},
			keepFirstMessages: 2,
			force:             false,
			expectEmpty:       false,
			description:       "Should not optimize when disabled and force=false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := createMockSDKClient(t, "Summary")

			optimizer := services.NewConversationOptimizer(services.OptimizerConfig{
				Enabled:           true,
				AutoAt:            80,
				BufferSize:        2,
				KeepFirstMessages: tt.keepFirstMessages,
				Client:            mockClient,
				Config:            &config.Config{},
				Tokenizer:         nil,
			})

			result := optimizer.OptimizeMessages(tt.messages, "anthropic/claude-3-5-sonnet-20241022", tt.force)

			if tt.expectEmpty {
				assert.Empty(t, result, tt.description)
			} else {
				assert.NotEmpty(t, result, tt.description)
			}
		})
	}
}

// Helper functions

func stringPtr(s string) *string {
	return &s
}

// createMockSDKClient creates a mock SDK client using counterfeiter that returns a canned summary response
func createMockSDKClient(t *testing.T, summaryText string) *mocks.FakeSDKClient {
	t.Helper()

	mockClient := &mocks.FakeSDKClient{}

	// Configure the mock to return a summary response
	content := sdk.NewMessageContent(summaryText)
	mockClient.GenerateContentReturns(&sdk.CreateChatCompletionResponse{
		Choices: []sdk.ChatCompletionChoice{
			{
				Message: sdk.Message{
					Role:    sdk.Assistant,
					Content: content,
				},
			},
		},
	}, nil)

	// Return self for chaining
	mockClient.WithOptionsReturns(mockClient)
	mockClient.WithMiddlewareOptionsReturns(mockClient)

	return mockClient
}

// validateNoOrphanedToolCalls checks that every assistant message with tool calls
// has all its corresponding tool responses, and every tool response has a corresponding
// assistant message with tool calls
func validateNoOrphanedToolCalls(t *testing.T, messages []sdk.Message) {
	t.Helper()

	expectedToolCallIDs, toolResponseIDs := collectToolCallIDs(messages)
	validateToolCallsHaveResponses(t, expectedToolCallIDs, toolResponseIDs, messages)
	validateToolResponsesHaveToolCalls(t, toolResponseIDs, expectedToolCallIDs, messages)
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
func validateToolCallsHaveResponses(t *testing.T, expectedToolCallIDs, toolResponseIDs map[string]int, messages []sdk.Message) {
	t.Helper()

	for toolCallID, assistantIdx := range expectedToolCallIDs {
		responseIdx, hasResponse := toolResponseIDs[toolCallID]
		require.True(t, hasResponse,
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
func validateToolResponsesHaveToolCalls(t *testing.T, toolResponseIDs, expectedToolCallIDs map[string]int, messages []sdk.Message) {
	t.Helper()

	for toolCallID, toolIdx := range toolResponseIDs {
		assistantIdx, hasToolCall := expectedToolCallIDs[toolCallID]
		require.True(t, hasToolCall,
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
