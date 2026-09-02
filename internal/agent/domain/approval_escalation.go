package domain

import (
	"context"

	sdk "github.com/inference-gateway/sdk"
)

// Escalation statuses returned by ApprovalEscalation.Escalate. They map onto
// distinguishable RequestApproval tool results so the driver knows why an
// escalation did not proceed: the call was not judge-rejected, it was already
// escalated once, or no interactive approver is reachable (headless/no-TTY).
const (
	EscalationApproved     = "approved"
	EscalationDenied       = "denied"
	EscalationNotRejected  = "not_rejected"
	EscalationAlreadyAsked = "already_asked"
	EscalationNoApprover   = "no_approver"
)

// ApprovalEscalationRequest is one ask-the-user-to-override-a-judge-rejection
// request: the rejected tool call to run, what the permission is needed for,
// and the agent's justification in its own words.
type ApprovalEscalationRequest struct {
	ToolCall sdk.ChatCompletionMessageToolCall
	What     string
	Why      string
}

// ApprovalEscalationResult is the user's decision. JudgeReason echoes the
// judge's rejection reason so the tool result can show the user and the judge
// both sides of the disagreement.
type ApprovalEscalationResult struct {
	Approved    bool
	Status      string
	JudgeReason string
}

// ApprovalEscalation lets the RequestApproval tool ask the user to override a
// judge rejection. It is implemented by the agent service, which owns the
// judge-rejection registry and prompts through the regular tool approval box;
// it is injected into the tool's execution context on the chat path only, so
// headless runs degrade to a distinguishable "no approver reachable" result.
type ApprovalEscalation interface {
	// Escalate validates eligibility (the call must be judge-rejected and not
	// yet escalated), prompts the user, and returns the decision.
	Escalate(ctx context.Context, req ApprovalEscalationRequest) (ApprovalEscalationResult, error)
}
