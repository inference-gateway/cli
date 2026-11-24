package tui

import (
	"fmt"
	"testing"

	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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

// TestAnApprovalIsAnsweredWhereItCameFrom pins where the approval window's
// answer goes, Esc's rejection included: a room's call to the room, and the
// chat's own call to the coordinator that unblocks the agent.
func TestAnApprovalIsAnsweredWhereItCameFrom(t *testing.T) {
	call := &sdk.ChatCompletionMessageToolCall{ID: "call-1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Write"}}
	tests := []struct {
		name  string
		state ApprovalUIState
		want  any
	}{
		{
			"a room's call answers the room",
			ApprovalUIState{PendingToolCall: call, Room: "release"},
			RoomApprovalAnsweredEvent{ToolCallID: "call-1", Action: agentdomain.ApprovalReject},
		},
		{
			"the chat's own call answers the agent",
			ApprovalUIState{PendingToolCall: call},
			agentdomain.ToolApprovalResponseEvent{Action: agentdomain.ApprovalReject, ToolCall: *call},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.state.Answer(agentdomain.ApprovalReject))
		})
	}
}
