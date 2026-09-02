// Judge-rejection escalation: the one-shot registry that tracks calls the LLM
// judge rejected and the bypass markers its user-approved escalations create
// (issue #1156). The RequestApproval tool escalates through the interactive
// question form; the judge approver honours bypass markers without a judge call.

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// judgeEscalationHint is appended to every judge rejection tool result so the
// driver learns the escalation path exists.
const judgeEscalationHint = "\n\nThe user can override this rejection: call the RequestApproval tool with the rejected call, what you need the permission for and why."

// The two question-form options of an escalation prompt; only picking the
// Approve label overrides the judge. A free-text "Other" answer is a denial
// whose text steers the agent's next step.
const (
	escalationApproveLabel = "Approve"
	escalationDenyLabel    = "Deny"
)

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

// state reports the tracked escalation for the call, if any.
func (j *judgeEscalations) state(name, args string) (judgeEscalation, bool) {
	key := escalationKey(name, args)
	j.mu.Lock()
	defer j.mu.Unlock()
	esc, ok := j.byKey[key]
	if !ok {
		return judgeEscalation{}, false
	}
	return *esc, true
}

// claim consumes the single escalation attempt for a rejected call, returning
// the judge's reason. ok=false when the call has no unconsumed rejection.
func (j *judgeEscalations) claim(name, args string) (string, bool) {
	key := escalationKey(name, args)
	j.mu.Lock()
	defer j.mu.Unlock()
	esc, ok := j.byKey[key]
	if !ok || esc.asked {
		return "", false
	}
	esc.asked = true
	return esc.reason, true
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
// whether one existed. Called by requestJudgeApproval ahead of the judge.
func (j *judgeEscalations) takeBypass(name, args string) bool {
	key := escalationKey(name, args)
	j.mu.Lock()
	defer j.mu.Unlock()
	esc, ok := j.byKey[key]
	if !ok || !esc.bypass {
		return false
	}
	esc.bypass = false
	return true
}

// approvalEscalator implements agentdomain.ApprovalEscalation for one tool
// execution. It reads the shared judge-rejection registry and prompts the user
// through the interactive question form (chatQuestionBroker), mirroring the
// AskUserQuestion flow.
type approvalEscalator struct {
	svc       *AgentServiceImpl
	publisher *eventPublisher
}

// Escalate answers the RequestApproval tool: is the call judge-rejected and not
// yet escalated (the attempt is consumed first), and what did the user decide?
// Only picking the Approve option overrides the judge; Deny or a free-text
// answer denies, with the text carried back to the agent.
func (e *approvalEscalator) Escalate(ctx context.Context, req agentdomain.ApprovalEscalationRequest) (agentdomain.ApprovalEscalationResult, error) {
	tc := req.ToolCall
	esc, exists := e.svc.escalations.state(tc.Function.Name, tc.Function.Arguments)
	if !exists {
		return agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationNotRejected}, nil
	}
	if esc.asked {
		return agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationAlreadyAsked, JudgeReason: esc.reason}, nil
	}

	reason, claimed := e.svc.escalations.claim(tc.Function.Name, tc.Function.Arguments)
	if !claimed {
		return agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationAlreadyAsked, JudgeReason: esc.reason}, nil
	}

	answers, ok, err := (&chatQuestionBroker{publisher: e.publisher}).
		AskUserQuestions(ctx, []agentdomain.UserQuestion{escalationQuestion(tc, req, reason)})
	if err != nil {
		return agentdomain.ApprovalEscalationResult{}, fmt.Errorf("approval escalation cancelled: %w", err)
	}
	if !ok || len(answers) == 0 {
		return agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationDenied, JudgeReason: reason}, nil
	}

	answer := answers[0]
	if slices.Contains(answer.SelectedLabels, escalationApproveLabel) {
		e.svc.escalations.approve(tc.Function.Name, tc.Function.Arguments)
		return agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationApproved, Approved: true, JudgeReason: reason}, nil
	}
	return agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationDenied, Answer: answer.OtherText, JudgeReason: reason}, nil
}

// escalationQuestion renders the approval question for the question form: the
// pending call, the judge's rejection reason (both sides), and the agent's
// what/why, with approve and deny options plus the UI's free-text choice.
func escalationQuestion(tc sdk.ChatCompletionMessageToolCall, req agentdomain.ApprovalEscalationRequest, judgeReason string) agentdomain.UserQuestion {
	question := fmt.Sprintf("The judge rejected %s(%s): %s\n\nNeeded for: %s\nBecause: %s\n\nApprove running this call once, bypassing the judge?",
		tc.Function.Name, tc.Function.Arguments, judgeReason, req.What, req.Why)
	return agentdomain.UserQuestion{
		Header:   "Approval",
		Question: question,
		Options: []agentdomain.UserQuestionOption{
			{Label: escalationApproveLabel, Description: fmt.Sprintf("Run %s once with the judge bypassed for this call", tc.Function.Name)},
			{Label: escalationDenyLabel, Description: "Reject it; any free text you add is sent back to the agent"},
		},
	}
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
