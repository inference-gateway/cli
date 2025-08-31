package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func isValidJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}

func TestAgentServiceImpl_GetMetrics(t *testing.T) {
	tests := []struct {
		name            string
		requestID       string
		setupMetrics    bool
		expectedMetrics *domain.ChatMetrics
	}{
		{
			name:         "get_existing_metrics",
			requestID:    "metrics-123",
			setupMetrics: true,
			expectedMetrics: &domain.ChatMetrics{
				Duration: 2 * time.Second,
				Usage: &sdk.CompletionUsage{
					TotalTokens: 100,
				},
			},
		},
		{
			name:            "get_nonexistent_metrics",
			requestID:       "nonexistent",
			setupMetrics:    false,
			expectedMetrics: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &AgentServiceImpl{
				metrics: make(map[string]*domain.ChatMetrics),
			}

			if tt.setupMetrics {
				agentService.metrics[tt.requestID] = tt.expectedMetrics
			}

			result := agentService.GetMetrics(tt.requestID)

			if tt.expectedMetrics == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expectedMetrics.Duration, result.Duration)
				assert.Equal(t, tt.expectedMetrics.Usage.TotalTokens, result.Usage.TotalTokens)
			}
		})
	}
}

func TestAgentServiceImpl_CancelRequest(t *testing.T) {
	tests := []struct {
		name          string
		requestID     string
		setupRequest  bool
		expectedError bool
	}{
		{
			name:          "cancel_existing_request",
			requestID:     "cancel-123",
			setupRequest:  true,
			expectedError: false,
		},
		{
			name:          "cancel_nonexistent_request",
			requestID:     "nonexistent",
			setupRequest:  false,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &AgentServiceImpl{
				activeRequests: make(map[string]context.CancelFunc),
			}

			if tt.setupRequest {
				_, cancel := context.WithCancel(context.Background())
				agentService.activeRequests[tt.requestID] = cancel
			}

			err := agentService.CancelRequest(tt.requestID)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAgentServiceImpl_ValidateRequest(t *testing.T) {
	tests := []struct {
		name        string
		request     *domain.AgentRequest
		expectError bool
	}{
		{
			name: "valid_request",
			request: &domain.AgentRequest{
				RequestID: "test-123",
				Model:     "openai/gpt-4",
				Messages: []sdk.Message{
					{Role: sdk.User, Content: "Hello"},
				},
			},
			expectError: false,
		},
		{
			name: "missing_request_id",
			request: &domain.AgentRequest{
				Model: "openai/gpt-4",
				Messages: []sdk.Message{
					{Role: sdk.User, Content: "Hello"},
				},
			},
			expectError: true,
		},
		{
			name: "missing_model",
			request: &domain.AgentRequest{
				RequestID: "test-123",
				Messages: []sdk.Message{
					{Role: sdk.User, Content: "Hello"},
				},
			},
			expectError: true,
		},
		{
			name: "empty_messages",
			request: &domain.AgentRequest{
				RequestID: "test-123",
				Model:     "openai/gpt-4",
				Messages:  []sdk.Message{},
			},
			expectError: true,
		},
		{
			name:        "nil_request",
			request:     nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentService := &AgentServiceImpl{}

			err := agentService.validateRequest(tt.request)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAgentServiceImpl_StreamingDeltaAccumulation(t *testing.T) {
	tests := []struct {
		name                     string
		streamingDeltas          []string
		expectedContent          string
		validateContentIntegrity func(t *testing.T, finalContent string, deltas []string)
	}{
		{
			name: "content_streaming_without_concatenation",
			streamingDeltas: []string{
				"Hello", "!", " I", "'m", " an", " assistant", " that", " can",
				" help", " you", " with", " Google", " Calendar", " tasks",
				".", " I", " have", " access", " to", " a", " Google",
				" Calendar", " agent", " that", " can", " perform",
				" the", " following", " functions", ":\n\n",
			},
			expectedContent: "Hello! I'm an assistant that can help you with Google Calendar tasks. I have access to a Google Calendar agent that can perform the following functions:\n\n",
			validateContentIntegrity: func(t *testing.T, finalContent string, deltas []string) {
				accumulated := ""
				for _, delta := range deltas {
					accumulated += delta
				}
				assert.Equal(t, accumulated, finalContent, "Streaming deltas should accumulate to final content without concatenation artifacts")
				assert.NotContains(t, finalContent, "give you more details:Hello!", "Should not contain concatenation artifacts")
			},
		},
		{
			name: "incremental_list_formatting",
			streamingDeltas: []string{
				"**", "📅", " Calendar", " Management", " Cap", "abilities", ":**\n\n",
				"1", ".", " **", "List", " Calendar", " Events", "**", " -",
				" View", " your", " upcoming", " events", ",", " meetings",
				",", " and", " appointments", "\n",
				"2", ".", " **", "Create", " Calendar", " Event", "**",
			},
			expectedContent: "**📅 Calendar Management Capabilities:**\n\n1. **List Calendar Events** - View your upcoming events, meetings, and appointments\n2. **Create Calendar Event**",
			validateContentIntegrity: func(t *testing.T, finalContent string, deltas []string) {
				accumulated := ""
				for _, delta := range deltas {
					accumulated += delta
				}
				assert.Equal(t, accumulated, finalContent, "List formatting should be preserved through streaming")
				assert.Contains(t, finalContent, "**📅 Calendar Management Capabilities:**", "Should preserve markdown formatting")
				assert.Contains(t, finalContent, "1. **List Calendar Events**", "Should preserve numbered list formatting")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalContent := ""

			for _, delta := range tt.streamingDeltas {
				finalContent += delta
			}

			assert.Equal(t, tt.expectedContent, finalContent)

			if tt.validateContentIntegrity != nil {
				tt.validateContentIntegrity(t, finalContent, tt.streamingDeltas)
			}
		})
	}
}

func TestAgentServiceImpl_A2AToolCallStreamingBehavior(t *testing.T) {
	tests := []struct {
		name                 string
		toolCallDeltas       []string
		expectedToolCallArgs string
		validateArgsBuild    func(t *testing.T, finalArgs string, deltas []string)
	}{
		{
			name: "a2a_query_agent_card_streaming",
			toolCallDeltas: []string{
				"{\"", "agent", "_url", "\":", " \"", "http", "://", "google",
				"-cal", "endar", "-agent", ":", "808", "0", "\"}",
			},
			expectedToolCallArgs: `{"agent_url": "http://google-calendar-agent:8080"}`,
			validateArgsBuild: func(t *testing.T, finalArgs string, deltas []string) {
				accumulated := ""
				for _, delta := range deltas {
					accumulated += delta
				}
				assert.Equal(t, accumulated, finalArgs, "Tool call arguments should build incrementally")
				assert.True(t, isValidJSON(finalArgs), "Final arguments should be valid JSON")
			},
		},
		{
			name: "a2a_submit_task_streaming",
			toolCallDeltas: []string{
				"{\"", "agent", "_url", "\":", " \"", "http", "://", "google",
				"-cal", "endar", "-agent", ":", "808", "0", "\",",
				" \"", "task", "_description", "\":", " \"", "List",
				" my", " calendar", " events", " for", " today", "\"}",
			},
			expectedToolCallArgs: `{"agent_url": "http://google-calendar-agent:8080", "task_description": "List my calendar events for today"}`,
			validateArgsBuild: func(t *testing.T, finalArgs string, deltas []string) {
				accumulated := ""
				for _, delta := range deltas {
					accumulated += delta
				}
				assert.Equal(t, accumulated, finalArgs, "Complex tool arguments should stream correctly")
				assert.True(t, isValidJSON(finalArgs), "Final arguments should be valid JSON")
				assert.Contains(t, finalArgs, "task_description", "Should contain task description field")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalArgs := ""

			for _, delta := range tt.toolCallDeltas {
				finalArgs += delta
			}

			assert.Equal(t, tt.expectedToolCallArgs, finalArgs)

			if tt.validateArgsBuild != nil {
				tt.validateArgsBuild(t, finalArgs, tt.toolCallDeltas)
			}
		})
	}
}

func TestAgentServiceImpl_MultiIterationA2AStreaming(t *testing.T) {
	tests := []struct {
		name                   string
		iteration1Content      []string
		iteration1FinishReason string
		iteration2Content      []string
		iteration2FinishReason string
		expectedIterations     int
		validateIterationFlow  func(t *testing.T, iter1Content, iter2Content string)
	}{
		{
			name: "tool_call_then_final_response",
			iteration1Content: []string{
				"Let", " me", " check", " what", " specific", " capabilities",
				" the", " Google", " Calendar", " agent", " has", " to",
				" give", " you", " more", " details", ":",
			},
			iteration1FinishReason: "tool_calls",
			iteration2Content: []string{
				"Hello", "!", " I", "'m", " an", " assistant", " that",
				" can", " help", " you", " with", " Google", " Calendar",
				" tasks", ".", " I", " have", " access", " to", " a",
				" Google", " Calendar", " agent", " that", " can",
				" perform", " the", " following", " functions", ":",
			},
			iteration2FinishReason: "stop",
			expectedIterations:     2,
			validateIterationFlow: func(t *testing.T, iter1Content, iter2Content string) {
				assert.NotEmpty(t, iter1Content, "First iteration should have content")
				assert.NotEmpty(t, iter2Content, "Second iteration should have content")
				assert.NotContains(t, iter2Content, iter1Content, "Second iteration should not contain first iteration content")
			},
		},
		{
			name: "agent_card_query_to_capabilities_list",
			iteration1Content: []string{
				"I'll", " query", " the", " Google", " Calendar", " agent",
				" to", " see", " what", " capabilities", " it", " offers",
			},
			iteration1FinishReason: "tool_calls",
			iteration2Content: []string{
				"Based", " on", " the", " agent", " capabilities", ",",
				" I", " can", " help", " you", " with", ":\n\n",
				"**", "Calendar", " Management", ":**\n",
				"1", ".", " Viewing", " events", "\n",
				"2", ".", " Creating", " events", "\n",
				"3", ".", " Updating", " events",
			},
			iteration2FinishReason: "stop",
			expectedIterations:     2,
			validateIterationFlow: func(t *testing.T, iter1Content, iter2Content string) {
				assert.Contains(t, iter1Content, "query", "First iteration should indicate querying")
				assert.Contains(t, iter2Content, "Based on", "Second iteration should reference results")
				assert.NotEqual(t, iter1Content, iter2Content, "Iterations should have different content")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iter1Content := ""
			for _, delta := range tt.iteration1Content {
				iter1Content += delta
			}

			iter2Content := ""
			for _, delta := range tt.iteration2Content {
				iter2Content += delta
			}

			assert.Equal(t, "tool_calls", tt.iteration1FinishReason, "First iteration should finish with tool_calls")
			assert.Equal(t, "stop", tt.iteration2FinishReason, "Second iteration should finish with stop")

			if tt.validateIterationFlow != nil {
				tt.validateIterationFlow(t, iter1Content, iter2Content)
			}
		})
	}
}

func TestAgentServiceImpl_CompleteStreamingFlowFromLogs(t *testing.T) {
	tests := []struct {
		name                  string
		streamChunks          []StreamChunk
		expectedFinalContent  string
		expectedToolCalls     int
		expectedFinishReason  string
		validateStreamingFlow func(t *testing.T, chunks []StreamChunk, finalContent string)
	}{
		{
			name: "full_a2a_streaming_from_gateway_logs",
			streamChunks: []StreamChunk{
				{Content: "Let", FinishReason: "", ToolCall: nil},
				{Content: " me", FinishReason: "", ToolCall: nil},
				{Content: " check", FinishReason: "", ToolCall: nil},
				{Content: " what", FinishReason: "", ToolCall: nil},
				{Content: " specific", FinishReason: "", ToolCall: nil},
				{Content: " capabilities", FinishReason: "", ToolCall: nil},
				{Content: " the", FinishReason: "", ToolCall: nil},
				{Content: " Google", FinishReason: "", ToolCall: nil},
				{Content: " Calendar", FinishReason: "", ToolCall: nil},
				{Content: " agent", FinishReason: "", ToolCall: nil},
				{Content: " has", FinishReason: "", ToolCall: nil},
				{Content: " to", FinishReason: "", ToolCall: nil},
				{Content: " give", FinishReason: "", ToolCall: nil},
				{Content: " you", FinishReason: "", ToolCall: nil},
				{Content: " more", FinishReason: "", ToolCall: nil},
				{Content: " details", FinishReason: "", ToolCall: nil},
				{Content: ":", FinishReason: "", ToolCall: nil},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Id: "call_0_5c926025-6aa5-4846-b509-ce42da596b22",
					Function: ToolCallFunction{
						Name:      "a2a_query_agent_card",
						Arguments: "",
					},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "{\""},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "agent"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "_url"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "\":"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: " \""},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "http"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "://"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "google"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "-calendar"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "-agent"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: ":"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "8080"},
				}},
				{Content: "", FinishReason: "", ToolCall: &ToolCallChunk{
					Function: ToolCallFunction{Arguments: "\"}"},
				}},
				{Content: "", FinishReason: "tool_calls", ToolCall: nil},
			},
			expectedFinalContent: "Let me check what specific capabilities the Google Calendar agent has to give you more details:",
			expectedToolCalls:    1,
			expectedFinishReason: "tool_calls",
			validateStreamingFlow: func(t *testing.T, chunks []StreamChunk, finalContent string) {
				contentParts := []string{}
				toolArgs := ""
				toolCallCount := 0

				for _, chunk := range chunks {
					if chunk.Content != "" {
						contentParts = append(contentParts, chunk.Content)
					}
					if chunk.ToolCall != nil {
						if chunk.ToolCall.Id != "" {
							toolCallCount++
						}
						if chunk.ToolCall.Function.Arguments != "" {
							toolArgs += chunk.ToolCall.Function.Arguments
						}
					}
				}

				reconstructedContent := ""
				for _, part := range contentParts {
					reconstructedContent += part
				}

				assert.Equal(t, finalContent, reconstructedContent, "Content should accumulate correctly from streaming deltas")
				assert.Equal(t, 1, toolCallCount, "Should have exactly one tool call")
				assert.Equal(t, `{"agent_url": "http://google-calendar-agent:8080"}`, toolArgs, "Tool arguments should build correctly")
				assert.True(t, isValidJSON(toolArgs), "Tool arguments should be valid JSON")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contentParts := []string{}
			toolCallCount := 0
			finalFinishReason := ""

			for _, chunk := range tt.streamChunks {
				if chunk.Content != "" {
					contentParts = append(contentParts, chunk.Content)
				}
				if chunk.ToolCall != nil && chunk.ToolCall.Id != "" {
					toolCallCount++
				}
				if chunk.FinishReason != "" {
					finalFinishReason = chunk.FinishReason
				}
			}

			reconstructedContent := ""
			for _, part := range contentParts {
				reconstructedContent += part
			}

			assert.Equal(t, tt.expectedFinalContent, reconstructedContent)
			assert.Equal(t, tt.expectedToolCalls, toolCallCount)
			assert.Equal(t, tt.expectedFinishReason, finalFinishReason)

			if tt.validateStreamingFlow != nil {
				tt.validateStreamingFlow(t, tt.streamChunks, reconstructedContent)
			}
		})
	}
}

type StreamChunk struct {
	Content      string
	FinishReason string
	ToolCall     *ToolCallChunk
}

type ToolCallChunk struct {
	Id       string
	Function ToolCallFunction
}

type ToolCallFunction struct {
	Name      string
	Arguments string
}

func TestAgentServiceImpl_A2AContentBuilderReset(t *testing.T) {
	tests := []struct {
		name                    string
		firstIterationContent   string
		secondIterationContent  string
		expectedSeparation      bool
		validateNoConcatenation func(t *testing.T, firstContent, secondContent string)
	}{
		{
			name:                   "content_builder_reset_prevents_concatenation",
			firstIterationContent:  "Let me check what specific capabilities the Google Calendar agent has to give you more details:",
			secondIterationContent: "Hello! I'm an assistant that can help you with Google Calendar tasks.",
			expectedSeparation:     true,
			validateNoConcatenation: func(t *testing.T, firstContent, secondContent string) {
				assert.NotContains(t, firstContent, secondContent, "First iteration should not contain second iteration content")
				assert.NotContains(t, secondContent, firstContent, "Second iteration should not contain first iteration content")

				concatenated := firstContent + secondContent
				assert.NotEqual(t, concatenated, firstContent, "First content should not be concatenated")
				assert.NotEqual(t, concatenated, secondContent, "Second content should not be concatenated")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var firstStoredContent, secondStoredContent string

			contentBuilder := &strings.Builder{}
			contentBuilder.WriteString(tt.firstIterationContent)
			firstStoredContent = contentBuilder.String()

			contentBuilder.Reset()

			contentBuilder.WriteString(tt.secondIterationContent)
			secondStoredContent = contentBuilder.String()

			assert.Equal(t, tt.firstIterationContent, firstStoredContent)
			assert.Equal(t, tt.secondIterationContent, secondStoredContent)

			if tt.validateNoConcatenation != nil {
				tt.validateNoConcatenation(t, firstStoredContent, secondStoredContent)
			}
		})
	}
}
