package conversation_test

import (
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	models "github.com/inference-gateway/cli/internal/platform/models"
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
						{ID: "call_A", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "read"}},
						{ID: "call_B", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "write"}},
					},
				},
				{Role: "tool", Content: sdk.NewMessageContent("file content"), ToolCallID: stringPtr("call_A")},
				{Role: "assistant", Content: sdk.NewMessageContent("Intermediate response")},
				{Role: "tool", Content: sdk.NewMessageContent("write success"), ToolCallID: stringPtr("call_B")},
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
						{ID: "call_A", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "read"}},
						{ID: "call_B", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "write"}},
					},
				},
				{Role: "tool", Content: sdk.NewMessageContent("file content"), ToolCallID: stringPtr("call_A")},
				{Role: "tool", Content: sdk.NewMessageContent("write success"), ToolCallID: stringPtr("call_B")},
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
						{ID: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "read"}},
					},
				},
				{Role: "tool", Content: sdk.NewMessageContent("file content"), ToolCallID: stringPtr("call_1")},
				{Role: "user", Content: sdk.NewMessageContent("Request 2")},
				{
					Role:    "assistant",
					Content: sdk.NewMessageContent("Using tools group 2"),
					ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
						{ID: "call_2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "write"}},
						{ID: "call_3", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "bash"}},
					},
				},
				{Role: "tool", Content: sdk.NewMessageContent("write success"), ToolCallID: stringPtr("call_2")},
				{Role: "tool", Content: sdk.NewMessageContent("bash output"), ToolCallID: stringPtr("call_3")},
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
						{ID: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "grep"}},
					},
				},
				{Role: "tool", Content: sdk.NewMessageContent("search results"), ToolCallID: stringPtr("call_1")},
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
						{ID: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool1"}},
						{ID: "call_2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool2"}},
					},
				},
				{Role: "tool", Content: sdk.NewMessageContent("result 1"), ToolCallID: stringPtr("call_1")},
				{Role: "user", Content: sdk.NewMessageContent("Next request")},
				{Role: "tool", Content: sdk.NewMessageContent("result 2"), ToolCallID: stringPtr("call_2")},
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
				{ID: "call_00_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool1"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallID: stringPtr("call_00_1")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 2"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{ID: "call_00_2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool2"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallID: stringPtr("call_00_2")},
			{Role: "user", Content: sdk.NewMessageContent("Request 2")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 3"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{ID: "call_00_3", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool3"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallID: stringPtr("call_00_3")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 4"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{ID: "call_00_4", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool4"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallID: stringPtr("call_00_4")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 5"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{ID: "call_00_5", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool5"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallID: stringPtr("call_00_5")},
			{Role: "user", Content: sdk.NewMessageContent("Request 3")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 6"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{ID: "call_00_6", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool6"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallID: stringPtr("call_00_6")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 7"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{ID: "call_00_7", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool7"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallID: stringPtr("call_00_7")},
			{Role: "user", Content: sdk.NewMessageContent("Request 4")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 8"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{ID: "call_00_8", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool8"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallID: stringPtr("call_00_8")},
			{Role: "assistant", Content: sdk.NewMessageContent("Let me use two tools"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{ID: "call_00_mw22yDQOyJlZQaFT2mmXnyBZ", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "read"}},
				{ID: "call_01_C9gJA1FoL22xfCrTHUWR2KYG", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "write"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("read result"), ToolCallID: stringPtr("call_00_mw22yDQOyJlZQaFT2mmXnyBZ")},
			{Role: "tool", Content: sdk.NewMessageContent("write result"), ToolCallID: stringPtr("call_01_C9gJA1FoL22xfCrTHUWR2KYG")},
			{Role: "assistant", Content: sdk.NewMessageContent("Response 9"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
				{ID: "call_00_9", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "tool9"}},
			}},
			{Role: "tool", Content: sdk.NewMessageContent("result"), ToolCallID: stringPtr("call_00_9")},
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
		toolCallIDs[tc.ID] = true
	}
	return toolCallIDs
}

// removeMatchingToolResponses removes tool call IDs that have responses
func removeMatchingToolResponses(result []sdk.Message, assistantIdx int, toolCallIDs map[string]bool) {
	for j := assistantIdx + 1; j < len(result); j++ {
		if result[j].Role == "tool" && result[j].ToolCallID != nil {
			delete(toolCallIDs, *result[j].ToolCallID)
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

			optimizer := conversation.NewConversationOptimizer(conversation.OptimizerConfig{
				Enabled:           true,
				AutoAt:            80,
				BufferSize:        2,
				KeepFirstMessages: tt.keepFirstMessages,
				Client:            mockClient,
				Config:            &config.Config{},
				Tokenizer:         nil,
			})

			result := optimizer.OptimizeMessages(tt.messages, "deepseek/deepseek-v4-pro", true)

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

			optimizer := conversation.NewConversationOptimizer(conversation.OptimizerConfig{
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

// TestOptimizeMessages_LastInputTokensTrigger verifies that OptimizeMessages
// uses the gateway-reported LastInputTokens from the wired-in repo, so
// auto-compaction fires when the actual prompt size (including system prompt
// and tool definitions) crosses the threshold - even though the entries-only
// estimate is far below it.
func TestOptimizeMessages_LastInputTokensTrigger(t *testing.T) {
	model := "moonshot/moonshot-v1-8k"

	models.SetGatewayContextWindows(map[string]int{model: 8192})
	t.Cleanup(func() { models.SetGatewayContextWindows(nil) })

	t.Run("fires when LastInputTokens above threshold", func(t *testing.T) {
		repo := conversation.NewInMemoryConversationRepository(nil, nil)
		require.NoError(t, repo.AddTokenUsage(model, 7000, 100, 7100, 0, 0))

		mockClient := createMockSDKClient(t, "Summary text")
		optimizer := conversation.NewConversationOptimizer(conversation.OptimizerConfig{
			Enabled:           true,
			AutoAt:            80,
			BufferSize:        2,
			KeepFirstMessages: 2,
			Client:            mockClient,
			Config:            &config.Config{},
			Tokenizer:         nil,
			Repo:              repo,
		})

		messages := []sdk.Message{
			{Role: "user", Content: sdk.NewMessageContent("hi")},
			{Role: "assistant", Content: sdk.NewMessageContent("hello")},
			{Role: "user", Content: sdk.NewMessageContent("again")},
			{Role: "assistant", Content: sdk.NewMessageContent("ack")},
			{Role: "user", Content: sdk.NewMessageContent("more")},
		}

		result := optimizer.OptimizeMessages(messages, model, false)

		assert.Equal(t, 1, mockClient.GenerateContentCallCount(),
			"summarizer should be invoked when LastInputTokens crosses threshold")
		assert.Less(t, len(result), len(messages),
			"compaction should reduce the message count")
	})

	t.Run("does not fire when LastInputTokens below threshold", func(t *testing.T) {
		repo := conversation.NewInMemoryConversationRepository(nil, nil)
		require.NoError(t, repo.AddTokenUsage(model, 1000, 100, 1100, 0, 0))

		mockClient := createMockSDKClient(t, "Summary text")
		optimizer := conversation.NewConversationOptimizer(conversation.OptimizerConfig{
			Enabled:           true,
			AutoAt:            80,
			BufferSize:        2,
			KeepFirstMessages: 2,
			Client:            mockClient,
			Config:            &config.Config{},
			Tokenizer:         nil,
			Repo:              repo,
		})

		messages := []sdk.Message{
			{Role: "user", Content: sdk.NewMessageContent("hi")},
			{Role: "assistant", Content: sdk.NewMessageContent("hello")},
			{Role: "user", Content: sdk.NewMessageContent("again")},
			{Role: "assistant", Content: sdk.NewMessageContent("ack")},
			{Role: "user", Content: sdk.NewMessageContent("more")},
		}

		result := optimizer.OptimizeMessages(messages, model, false)

		assert.Equal(t, 0, mockClient.GenerateContentCallCount(),
			"summarizer must not be invoked when LastInputTokens is below threshold")
		assert.Equal(t, len(messages), len(result),
			"messages should be returned unchanged when gate does not fire")
	})
}

// TestOptimizeMessages_NoAutoCompactForUnknownModel verifies that a model with
// no configured context window does not auto-compact (force=false), even when
// the gateway-reported token count is huge. Without this guard the optimizer
// would measure fullness against the default fallback window and summarize on
// nearly every turn (this was the minimax-m3 bug, before that model was added
// to the registry).
func TestOptimizeMessages_NoAutoCompactForUnknownModel(t *testing.T) {
	model := "ollama_cloud/some-unlisted-model"

	repo := conversation.NewInMemoryConversationRepository(nil, nil)
	require.NoError(t, repo.AddTokenUsage(model, 500000, 100, 500100, 0, 0))

	mockClient := createMockSDKClient(t, "Summary text")
	optimizer := conversation.NewConversationOptimizer(conversation.OptimizerConfig{
		Enabled:           true,
		AutoAt:            80,
		BufferSize:        2,
		KeepFirstMessages: 2,
		Client:            mockClient,
		Config:            &config.Config{},
		Tokenizer:         nil,
		Repo:              repo,
	})

	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("hi")},
		{Role: "assistant", Content: sdk.NewMessageContent("hello")},
		{Role: "user", Content: sdk.NewMessageContent("again")},
		{Role: "assistant", Content: sdk.NewMessageContent("ack")},
		{Role: "user", Content: sdk.NewMessageContent("more")},
	}

	result := optimizer.OptimizeMessages(messages, model, false)

	assert.Equal(t, 0, mockClient.GenerateContentCallCount(),
		"summarizer must not run for a model with no configured context window")
	assert.Equal(t, len(messages), len(result),
		"messages must be returned unchanged when the context window is unknown")
}

// TestOptimizeMessages_ForcedCompactWorksForUnknownModel verifies that a forced
// compaction (manual /compact, or session rollover) still runs for a model with
// no configured context window - only the automatic threshold path is gated.
func TestOptimizeMessages_ForcedCompactWorksForUnknownModel(t *testing.T) {
	model := "ollama_cloud/some-unlisted-model"

	repo := conversation.NewInMemoryConversationRepository(nil, nil)

	mockClient := createMockSDKClient(t, "Summary text")
	optimizer := conversation.NewConversationOptimizer(conversation.OptimizerConfig{
		Enabled:           true,
		AutoAt:            80,
		BufferSize:        2,
		KeepFirstMessages: 2,
		Client:            mockClient,
		Config:            &config.Config{},
		Tokenizer:         nil,
		Repo:              repo,
	})

	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("hi")},
		{Role: "assistant", Content: sdk.NewMessageContent("hello")},
		{Role: "user", Content: sdk.NewMessageContent("again")},
		{Role: "assistant", Content: sdk.NewMessageContent("ack")},
		{Role: "user", Content: sdk.NewMessageContent("more")},
	}

	result := optimizer.OptimizeMessages(messages, model, true)

	assert.Equal(t, 1, mockClient.GenerateContentCallCount(),
		"forced compaction must run even when the context window is unknown")
	assert.Less(t, len(result), len(messages),
		"forced compaction should reduce the message count")
}

// Helper functions

func stringPtr(s string) *string {
	return &s
}

// createMockSDKClient creates a mock SDK client using counterfeiter that returns a canned summary response
func createMockSDKClient(t *testing.T, summaryText string) *sdkmocks.FakeClient {
	t.Helper()

	mockClient := &sdkmocks.FakeClient{}

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
				if tc.ID != "" {
					expectedToolCallIDs[tc.ID] = i
				}
			}
		} else if msg.Role == "tool" && msg.ToolCallID != nil {
			toolResponseIDs[*msg.ToolCallID] = i
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

		if messages[j].Role == "tool" && messages[j].ToolCallID != nil {
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

	toolCallID := *messages[toolResponseIdx].ToolCallID
	matchesOurCall := false

	for _, tc := range toolCalls {
		if tc.ID == toolCallID {
			matchesOurCall = true
			foundResponses[tc.ID] = true
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
		if tc.ID != "" {
			assert.True(t, foundResponses[tc.ID],
				"Tool call %s from assistant at index %d has no response in the immediate following messages",
				tc.ID, assistantIdx)
		}
	}
}

func newForcedOptimizer(t *testing.T, client *sdkmocks.FakeClient, restore func([]sdk.Message) []sdk.Message) convdomain.ConversationOptimizer {
	t.Helper()
	return conversation.NewConversationOptimizer(conversation.OptimizerConfig{
		Enabled:           true,
		AutoAt:            80,
		KeepFirstMessages: 2,
		Client:            client,
		Config:            &config.Config{},
		Repo:              conversation.NewInMemoryConversationRepository(nil, nil),
		RestoreContext:    restore,
	})
}

func TestOptimizeMessages_SummaryIsUserCheckpoint(t *testing.T) {
	optimizer := newForcedOptimizer(t, createMockSDKClient(t, "## Objective\n- ship"), nil)
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("hi")},
		{Role: "assistant", Content: sdk.NewMessageContent("hello")},
		{Role: "user", Content: sdk.NewMessageContent("again")},
		{Role: "assistant", Content: sdk.NewMessageContent("ack")},
	}

	result := optimizer.OptimizeMessages(messages, "openai/gpt", true)

	require.Len(t, result, 3)
	assert.Equal(t, sdk.User, result[2].Role)
	text, _ := result[2].Content.AsMessageContent0()
	assert.Contains(t, text, "--- Context Summary ---")
	assert.Contains(t, text, "## Objective")
}

func TestOptimizeMessages_EmptySummaryKeepsOriginal(t *testing.T) {
	optimizer := newForcedOptimizer(t, createMockSDKClient(t, "  \n"), nil)
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("hi")},
		{Role: "assistant", Content: sdk.NewMessageContent("hello")},
		{Role: "user", Content: sdk.NewMessageContent("again")},
	}

	result := optimizer.OptimizeMessages(messages, "openai/gpt", true)

	assert.Equal(t, messages, result, "an empty summary must never drop history")
}

func TestOptimizeMessages_HeadWithoutUserTurnIsNotKept(t *testing.T) {
	optimizer := newForcedOptimizer(t, createMockSDKClient(t, "summary"), nil)
	messages := []sdk.Message{
		{Role: "assistant", Content: sdk.NewMessageContent("reading"), ToolCalls: &[]sdk.ChatCompletionMessageToolCall{
			{ID: "call_1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}},
		}},
		{Role: "tool", Content: sdk.NewMessageContent("content"), ToolCallID: stringPtr("call_1")},
		{Role: "assistant", Content: sdk.NewMessageContent("done")},
		{Role: "user", Content: sdk.NewMessageContent("next")},
	}

	result := optimizer.OptimizeMessages(messages, "openai/gpt", true)

	require.Len(t, result, 1)
	assert.Equal(t, sdk.User, result[0].Role, "a compacted conversation must open with the checkpoint, never a tool call")
}

func TestOptimizeMessages_RestoreHookAppendsAfterSummary(t *testing.T) {
	var seen []sdk.Message
	restore := func(summarized []sdk.Message) []sdk.Message {
		seen = summarized
		return []sdk.Message{{Role: "assistant", Content: sdk.NewMessageContent("restored")}}
	}
	optimizer := newForcedOptimizer(t, createMockSDKClient(t, "summary"), restore)
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("hi")},
		{Role: "assistant", Content: sdk.NewMessageContent("hello")},
		{Role: "user", Content: sdk.NewMessageContent("again")},
		{Role: "assistant", Content: sdk.NewMessageContent("ack")},
	}

	result := optimizer.OptimizeMessages(messages, "openai/gpt", true)

	require.Len(t, result, 4)
	assert.Equal(t, sdk.User, result[2].Role)
	text, _ := result[3].Content.AsMessageContent0()
	assert.Equal(t, "restored", text)
	assert.Equal(t, messages[2:], seen, "the hook sees only the summarized range")
}

func TestGenerateLLMSummary_UsesCheckpointTemplate(t *testing.T) {
	client := createMockSDKClient(t, "summary")
	optimizer := newForcedOptimizer(t, client, nil)
	messages := []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("hi")},
		{Role: "assistant", Content: sdk.NewMessageContent("hello")},
		{Role: "user", Content: sdk.NewMessageContent("again")},
	}

	optimizer.OptimizeMessages(messages, "openai/gpt", true)

	require.Equal(t, 1, client.GenerateContentCallCount())
	_, _, _, sent := client.GenerateContentArgsForCall(0)
	system, _ := sent[0].Content.AsMessageContent0()
	for _, heading := range []string{"## Objective", "## Important Details", "## Work State", "## Next Move", "## Relevant Files"} {
		assert.Contains(t, system, heading)
	}
}

// TestOptimizeMessages_PointerStrategySkipsSummarizer pins the pointer strategy:
// the checkpoint is one message pointing the model at the previous session and
// no LLM call is made. The kept head survives verbatim.
func TestOptimizeMessages_PointerStrategySkipsSummarizer(t *testing.T) {
	repo := &convmocks.FakeConversationRepository{}
	repo.GetCurrentConversationIDReturns("11111111-2222-4333-8444-555566667777")
	mockClient := createMockSDKClient(t, "LLM would-be summary")
	optimizer := conversation.NewConversationOptimizer(conversation.OptimizerConfig{
		Enabled:           true,
		AutoAt:            80,
		KeepFirstMessages: 2,
		Client:            mockClient,
		Config:            &config.Config{},
		Strategy:          config.CompactStrategyPointer,
		Repo:              repo,
	})
	messages := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("hello")},
		{Role: sdk.User, Content: sdk.NewMessageContent("again")},
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("ack")},
	}

	result := optimizer.OptimizeMessages(messages, "openai/gpt", true)

	require.Len(t, result, 3, "the kept head plus the pointer, nothing else")
	assert.Equal(t, messages[:2], result[:2], "the kept head must survive verbatim")
	assert.Equal(t, sdk.User, result[2].Role)
	text, _ := result[2].Content.AsMessageContent0()
	assert.True(t, strings.HasPrefix(text, "--- Context Summary ---"),
		"the pointer must open with the checkpoint marker so hasCheckpoint keeps detecting it")
	assert.Contains(t, text, "session `11111111-2222-4333-8444-555566667777`")
	assert.Contains(t, text, "infer conversations show 11111111-2222-4333-8444-555566667777 --grep")
	assert.NotContains(t, text, "--- End Summary ---", "no LLM summary is embedded")
	assert.Equal(t, 0, mockClient.GenerateContentCallCount(), "pointer mode must not call the summarizer")
}

// TestOptimizeMessages_PointerStrategyWithRestoreHook pins that the restore
// hook runs in pointer mode too and lands after the pointer message. No client
// is wired at all - pointer mode must work without one.
func TestOptimizeMessages_PointerStrategyWithRestoreHook(t *testing.T) {
	repo := &convmocks.FakeConversationRepository{}
	repo.GetCurrentConversationIDReturns("99999999-8888-4777-8666-111122223333")
	optimizer := conversation.NewConversationOptimizer(conversation.OptimizerConfig{
		Enabled:           true,
		AutoAt:            80,
		KeepFirstMessages: 2,
		Config:            &config.Config{},
		Strategy:          config.CompactStrategyPointer,
		Repo:              repo,
		RestoreContext: func([]sdk.Message) []sdk.Message {
			return []sdk.Message{{Role: sdk.Assistant, Content: sdk.NewMessageContent("restored skill call")}}
		},
	})
	messages := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("hello")},
		{Role: sdk.User, Content: sdk.NewMessageContent("again")},
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("ack")},
	}

	result := optimizer.OptimizeMessages(messages, "openai/gpt", true)

	require.Len(t, result, 4, "kept head + pointer + restore hook output")
	pointer, _ := result[2].Content.AsMessageContent0()
	assert.Contains(t, pointer, "99999999-8888-4777-8666-111122223333")
	restored, _ := result[3].Content.AsMessageContent0()
	assert.Equal(t, "restored skill call", restored, "the restore hook must run in pointer mode too")
}

// TestOptimizeMessages_PointerStrategyWithoutSessionIDKeepsOriginal pins the
// safe fallback: without a session id to point at, the history must survive
// untouched rather than get dropped behind a broken pointer.
func TestOptimizeMessages_PointerStrategyWithoutSessionIDKeepsOriginal(t *testing.T) {
	mockClient := createMockSDKClient(t, "summary")
	optimizer := conversation.NewConversationOptimizer(conversation.OptimizerConfig{
		Enabled:           true,
		AutoAt:            80,
		KeepFirstMessages: 2,
		Client:            mockClient,
		Config:            &config.Config{},
		Strategy:          config.CompactStrategyPointer,
		Repo:              conversation.NewInMemoryConversationRepository(nil, nil),
	})
	messages := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("hi")},
		{Role: sdk.Assistant, Content: sdk.NewMessageContent("hello")},
		{Role: sdk.User, Content: sdk.NewMessageContent("again")},
	}

	result := optimizer.OptimizeMessages(messages, "openai/gpt", true)

	assert.Equal(t, messages, result, "a broken pointer must never drop history")
	assert.Equal(t, 0, mockClient.GenerateContentCallCount())
}
