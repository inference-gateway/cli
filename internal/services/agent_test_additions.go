package services

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	assert "github.com/stretchr/testify/assert"
)

func TestEventPublisher_PublishToolExecutionCompleted(t *testing.T) {
	tests := []struct {
		name            string
		results         []domain.ConversationEntry
		expectedSuccess int
		expectedFailure int
		expectedTotal   int
		expectedResults int
		expectEventSent bool
	}{
		{
			name: "all_tools_succeed",
			results: []domain.ConversationEntry{
				{ToolExecution: &domain.ToolExecutionResult{Success: true, ToolName: "Read"}},
				{ToolExecution: &domain.ToolExecutionResult{Success: true, ToolName: "Write"}},
			},
			expectedSuccess: 2,
			expectedFailure: 0,
			expectedTotal:   2,
			expectedResults: 2,
			expectEventSent: true,
		},
		{
			name: "all_tools_fail",
			results: []domain.ConversationEntry{
				{ToolExecution: &domain.ToolExecutionResult{Success: false, ToolName: "Bash", Error: "failed"}},
			},
			expectedSuccess: 0,
			expectedFailure: 1,
			expectedTotal:   1,
			expectedResults: 1,
			expectEventSent: true,
		},
		{
			name: "mixed_success_and_failure",
			results: []domain.ConversationEntry{
				{ToolExecution: &domain.ToolExecutionResult{Success: true, ToolName: "Read"}},
				{ToolExecution: &domain.ToolExecutionResult{Success: false, ToolName: "Write", Error: "permission denied"}},
				{ToolExecution: &domain.ToolExecutionResult{Success: true, ToolName: "Grep"}},
			},
			expectedSuccess: 2,
			expectedFailure: 1,
			expectedTotal:   3,
			expectedResults: 3,
			expectEventSent: true,
		},
		{
			name:            "empty_results",
			results:         []domain.ConversationEntry{},
			expectedSuccess: 0,
			expectedFailure: 0,
			expectedTotal:   0,
			expectedResults: 0,
			expectEventSent: true,
		},
		{
			name: "results_with_nil_tool_execution",
			results: []domain.ConversationEntry{
				{ToolExecution: nil},
				{ToolExecution: &domain.ToolExecutionResult{Success: true, ToolName: "Read"}},
			},
			expectedSuccess: 1,
			expectedFailure: 0,
			expectedTotal:   2,
			expectedResults: 1, // Only one has ToolExecution
			expectEventSent: true,
		},
		{
			name: "multiple_failures_with_errors",
			results: []domain.ConversationEntry{
				{ToolExecution: &domain.ToolExecutionResult{Success: false, ToolName: "Write", Error: "disk full"}},
				{ToolExecution: &domain.ToolExecutionResult{Success: false, ToolName: "Bash", Error: "command not found"}},
				{ToolExecution: &domain.ToolExecutionResult{Success: false, ToolName: "Edit", Error: "file not found"}},
			},
			expectedSuccess: 0,
			expectedFailure: 3,
			expectedTotal:   3,
			expectedResults: 3,
			expectEventSent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			chatEvents := make(chan domain.ChatEvent, 10)
			publisher := newEventPublisher("test-request-123", chatEvents)

			// Execute
			publisher.publishToolExecutionCompleted(tt.results)

			// Assert
			select {
			case event := <-chatEvents:
				if !tt.expectEventSent {
					t.Fatal("Expected no event, but received one")
				}

				completedEvent, ok := event.(domain.ToolExecutionCompletedEvent)
				if !ok {
					t.Fatalf("Expected ToolExecutionCompletedEvent, got %T", event)
				}

				assert.Equal(t, "test-request-123", completedEvent.RequestID)
				assert.Equal(t, "test-request-123", completedEvent.SessionID)
				assert.Equal(t, tt.expectedSuccess, completedEvent.SuccessCount)
				assert.Equal(t, tt.expectedFailure, completedEvent.FailureCount)
				assert.Equal(t, tt.expectedTotal, completedEvent.TotalExecuted)
				assert.Len(t, completedEvent.Results, tt.expectedResults)
				assert.False(t, completedEvent.Timestamp.IsZero())

				// Verify that only entries with non-nil ToolExecution are included
				for _, result := range completedEvent.Results {
					assert.NotNil(t, result)
				}
			case <-time.After(100 * time.Millisecond):
				if tt.expectEventSent {
					t.Fatal("Expected event to be sent, but timed out")
				}
			}
		})
	}
}
