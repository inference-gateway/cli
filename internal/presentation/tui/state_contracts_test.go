package tui

import (
	"fmt"
	"testing"
)

func TestStateEnumStrings(t *testing.T) {
	tests := []struct {
		v    fmt.Stringer
		want string
	}{
		{ChatStatusIdle, "Idle"},
		{ChatStatusStarting, "Starting"},
		{ChatStatusThinking, "Thinking"},
		{ChatStatusGenerating, "Generating"},
		{ChatStatusReceivingTools, "ReceivingTools"},
		{ChatStatusWaitingTools, "WaitingTools"},
		{ChatStatusCompleted, "Completed"},
		{ChatStatusError, "Error"},
		{ChatStatusCancelled, "Cancelled"},
		{ChatStatus(99), "Unknown"},
		{ToolCallStatusPending, "Pending"},
		{ToolCallStatusWaitingApproval, "WaitingApproval"},
		{ToolCallStatusExecuting, "Executing"},
		{ToolCallStatusCompleted, "Completed"},
		{ToolCallStatusFailed, "Failed"},
		{ToolCallStatusCancelled, "Cancelled"},
		{ToolCallStatusDenied, "Denied"},
		{ToolCallStatus(99), "Unknown"},
		{ToolExecutionStatusIdle, "Idle"},
		{ToolExecutionStatusProcessing, "Processing"},
		{ToolExecutionStatusExecuting, "Executing"},
		{ToolExecutionStatusCompleted, "Completed"},
		{ToolExecutionStatusFailed, "Failed"},
		{ToolExecutionStatus(99), "Unknown"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T/%s", tt.v, tt.want), func(t *testing.T) {
			if got := tt.v.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
