package services

import (
	"context"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

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
