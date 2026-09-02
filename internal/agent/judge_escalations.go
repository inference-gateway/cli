package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// judgeEscalationHint is appended to every judge rejection tool result so the
// driver learns the escalation path exists.
const judgeEscalationHint = "\n\nThe judge is advisory, not a hard block: the user can override it. If the user explicitly asked for this action, call the RequestApproval tool now with the rejected call, what you need the permission for and why."

// judgeEscalation is one judge-rejected tool call that the agent may escalate
// to the user via RequestApproval.
type judgeEscalation struct {
	reason string
	asked  bool
	bypass bool
}

// judgeEscalations tracks judge-rejected tool calls keyed by tool name and
// canonical arguments. Each rejected call may be escalated once; an approved
// escalation sets a bypass marker that makes the next matching judge decision
// approve without consulting the judge (that single invocation only).
//
// ponytail: entries live for the session and are never pruned; add eviction
// if a long session ever accumulates thousands of distinct rejections.
type judgeEscalations struct {
	mu    sync.Mutex
	byKey map[string]*judgeEscalation
}

func newJudgeEscalations() *judgeEscalations {
	return &judgeEscalations{byKey: make(map[string]*judgeEscalation)}
}

// escalationKey identifies a tool call by name and canonical arguments so a
// re-issued call with reformatted JSON (whitespace, key order) still matches.
func escalationKey(name, argsJSON string) string {
	var args any
	if err := json.Unmarshal([]byte(argsJSON), &args); err == nil {
		if canonical, err := json.Marshal(args); err == nil {
			argsJSON = string(canonical)
		}
	}
	return name + "\x00" + argsJSON
}

// record stores a fresh judge rejection for the call. Re-rejections of an
// already-tracked call keep the original entry so the one-escalation rule stands.
func (j *judgeEscalations) record(name, args, reason string) {
	key := escalationKey(name, args)
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, exists := j.byKey[key]; !exists {
		j.byKey[key] = &judgeEscalation{reason: reason}
	}
}

// claim consumes the single escalation attempt for a rejected call and returns
// the judge's reason. status is "" on success, EscalationNotRejected when the
// call was never rejected, or EscalationAlreadyAsked when the attempt is spent.
func (j *judgeEscalations) claim(name, args string) (reason, status string) {
	key := escalationKey(name, args)
	j.mu.Lock()
	defer j.mu.Unlock()
	esc, ok := j.byKey[key]
	if !ok {
		return "", agentdomain.EscalationNotRejected
	}
	if esc.asked {
		return esc.reason, agentdomain.EscalationAlreadyAsked
	}
	esc.asked = true
	return esc.reason, ""
}

// approve records the user's approval; takeBypass consumes it once.
func (j *judgeEscalations) approve(name, args string) {
	key := escalationKey(name, args)
	j.mu.Lock()
	defer j.mu.Unlock()
	if esc, ok := j.byKey[key]; ok {
		esc.bypass = true
	}
}

// takeBypass consumes the approval marker recorded for this call, reporting
// whether one existed. Called by requestJudgeApproval ahead of the judge. The
// entry is dropped so a later judge rejection of the same call starts fresh
// and can be escalated again; only denials stick for the session.
func (j *judgeEscalations) takeBypass(name, args string) bool {
	key := escalationKey(name, args)
	j.mu.Lock()
	defer j.mu.Unlock()
	esc, ok := j.byKey[key]
	if !ok || !esc.bypass {
		return false
	}
	delete(j.byKey, key)
	return true
}

// approvalEscalator implements agentdomain.ApprovalEscalation for one tool
// execution. It reads the shared judge-rejection registry and prompts the user
// through the regular tool approval box (requestHumanApproval), so an
// escalation looks and behaves like any other gated call.
type approvalEscalator struct {
	svc       *AgentServiceImpl
	publisher *eventPublisher
}

// Escalate answers the RequestApproval tool: is the call judge-rejected and not
// yet escalated (the attempt is consumed first), and what did the user decide?
// Approve (or Auto-Approve) overrides the judge for one invocation; Reject,
// dismissal, or a timeout denies.
func (e *approvalEscalator) Escalate(ctx context.Context, req agentdomain.ApprovalEscalationRequest) (agentdomain.ApprovalEscalationResult, error) {
	tc := req.ToolCall
	reason, status := e.svc.escalations.claim(tc.Function.Name, tc.Function.Arguments)
	if status != "" {
		return agentdomain.ApprovalEscalationResult{Status: status, JudgeReason: reason}, nil
	}

	// The escalated call gets its own ID so the pending entry the TUI records
	// for the approval box never collides with the RequestApproval call itself.
	tc.ID = agentdomain.GetToolCallID(ctx) + "-escalation"
	approved, _, err := e.svc.requestHumanApproval(ctx, tc, e.publisher, escalationNote(req, reason))
	if err != nil {
		return agentdomain.ApprovalEscalationResult{}, fmt.Errorf("escalation request failed: %w", err)
	}
	if !approved {
		return agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationDenied, JudgeReason: reason}, nil
	}
	e.svc.escalations.approve(tc.Function.Name, tc.Function.Arguments)
	return agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationApproved, Approved: true, JudgeReason: reason}, nil
}

// escalationNote is the context block shown above the call in the approval
// box: the judge's rejection reason (both sides) and the agent's what/why.
func escalationNote(req agentdomain.ApprovalEscalationRequest, judgeReason string) string {
	return fmt.Sprintf("Rejected by judge: %s\nNeeded for: %s\nBecause: %s\nApprove runs this call once with the judge bypassed.",
		judgeReason, req.What, req.Why)
}

// consumeJudgeBypass checks the shared registry for a user-approved escalation
// of this call and consumes it. Used by requestJudgeApproval to skip the judge
// for the single approved invocation.
func (s *AgentServiceImpl) consumeJudgeBypass(tc sdk.ChatCompletionMessageToolCall) bool {
	if s.escalations == nil {
		return false
	}
	if s.escalations.takeBypass(tc.Function.Name, tc.Function.Arguments) {
		logger.Info("running judge-rejected call approved by user via RequestApproval", "tool", tc.Function.Name)
		return true
	}
	return false
}
