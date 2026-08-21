package domain

import (
	"context"

	sdk "github.com/inference-gateway/sdk"
)

// ApprovalPolicy determines whether a tool execution requires user approval
// This interface allows for different approval strategies (standard, permissive, strict, etc.)
// Implementations define the business rules for when user approval is required before
// executing potentially dangerous or state-changing operations.
type ApprovalPolicy interface {
	// ShouldRequireApproval returns true if the tool execution requires user approval
	// ctx: context for the approval decision
	// toolCall: the tool being invoked with its arguments
	// isChatMode: whether execution is in interactive chat mode
	ShouldRequireApproval(ctx context.Context, toolCall *sdk.ChatCompletionMessageToolCall, isChatMode bool) bool
}
