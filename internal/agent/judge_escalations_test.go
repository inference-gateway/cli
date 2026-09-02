package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	conversationmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// stubJudgeApprover answers every judge call with a canned verdict.
type stubJudgeApprover struct {
	verdict agentdomain.JudgeVerdict
}

func (s stubJudgeApprover) Judge(ctx context.Context, in agentdomain.JudgeInput) (agentdomain.JudgeVerdict, error) {
	return s.verdict, nil
}

// escalationToolCall returns the Bash call the escalation tests track.
func escalationToolCall(id string) sdk.ChatCompletionMessageToolCall {
	return sdk.ChatCompletionMessageToolCall{
		ID: id,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Bash",
			Arguments: `{"command":"git push"}`,
		},
	}
}

func TestJudgeEscalationsLifecycle(t *testing.T) {
	j := newJudgeEscalations()

	if _, exists := j.state("Bash", `{"command":"git push"}`); exists {
		t.Fatal("no escalation should exist before a rejection is recorded")
	}

	j.record("Bash", `{"command":"git push"}`, "needs context")
	esc, exists := j.state("Bash", `{"command":  "git push"}`)
	if !exists {
		t.Fatal("reformatted arguments must resolve to the tracked key")
	}
	if esc.reason != "needs context" || esc.asked || esc.bypass {
		t.Fatalf("fresh rejection = %+v, want reason-only", esc)
	}

	j.record("Bash", `{"command":"git push"}`, "second rejection")
	if again, _ := j.state("Bash", `{"command":"git push"}`); again.reason != "needs context" {
		t.Fatalf("re-rejections must keep the original entry, got reason %q", again.reason)
	}

	reason, ok := j.claim("Bash", `{"command":"git push"}`)
	if !ok || reason != "needs context" {
		t.Fatalf("claim = (%q, %v), want the judge reason", reason, ok)
	}
	if _, ok := j.claim("Bash", `{"command":"git push"}`); ok {
		t.Fatal("the escalation attempt must be consumed after the first claim")
	}

	j.approve("Bash", `{"command":"git push"}`)
	if !j.takeBypass("Bash", `{"command": "git push"}`) {
		t.Fatal("an approved escalation must arm the bypass marker")
	}
	if j.takeBypass("Bash", `{"command":"git push"}`) {
		t.Fatal("the bypass marker must be consumed on first use")
	}

	if _, ok := j.claim("Read", "{}"); ok {
		t.Fatal("claiming an untracked call must fail")
	}
}

func TestEscalationKeyCanonicalizesArguments(t *testing.T) {
	if escalationKey("Bash", `{"command":"git push"}`) != escalationKey("Bash", `{ "command" : "git push" }`) {
		t.Fatal("whitespace differences must not change the escalation key")
	}
	if escalationKey("Bash", `{"a":1,"command":"x"}`) != escalationKey("Bash", `{"command":"x","a":1}`) {
		t.Fatal("key-order differences must not change the escalation key")
	}
	if escalationKey("Bash", `{"command":"git push"}`) == escalationKey("Bash", `{"command":"git pull"}`) {
		t.Fatal("different arguments must produce different keys")
	}
	if escalationKey("Bash", `{"command":"git push"}`) == escalationKey("Read", `{"command":"git push"}`) {
		t.Fatal("different tools must produce different keys")
	}
}

// escalateAsync runs Escalate on a goroutine and returns the outcome channel;
// the caller drives the published question form from the event channel.
func escalateAsync(t *testing.T, esc *approvalEscalator, req agentdomain.ApprovalEscalationRequest) chan struct {
	res agentdomain.ApprovalEscalationResult
	err error
} {
	t.Helper()
	outcome := make(chan struct {
		res agentdomain.ApprovalEscalationResult
		err error
	}, 1)
	go func() {
		res, err := esc.Escalate(context.Background(), req)
		outcome <- struct {
			res agentdomain.ApprovalEscalationResult
			err error
		}{res, err}
	}()
	return outcome
}

// awaitQuestion waits for the published question form and fails the test on timeout.
func awaitQuestion(t *testing.T, events <-chan agentdomain.ChatEvent) agentdomain.UserQuestionRequestedEvent {
	t.Helper()
	select {
	case e := <-events:
		evt, ok := e.(agentdomain.UserQuestionRequestedEvent)
		if !ok {
			t.Fatalf("expected UserQuestionRequestedEvent, got %T", e)
		}
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("no approval question was published")
		return agentdomain.UserQuestionRequestedEvent{}
	}
}

func TestApprovalEscalatorApprovedSetsBypass(t *testing.T) {
	svc := &AgentServiceImpl{escalations: newJudgeEscalations()}
	svc.escalations.record("Bash", `{"command":"git push"}`, "needs context")

	events := make(chan agentdomain.ChatEvent, 8)
	pub := &eventPublisher{requestID: "req-1", chatEvents: events}
	esc := &approvalEscalator{svc: svc, publisher: pub}

	outcome := escalateAsync(t, esc, agentdomain.ApprovalEscalationRequest{
		ToolCall: escalationToolCall("tc-1"),
		What:     "push the feature branch",
		Why:      "the user asked me to ship it",
	})

	evt := awaitQuestion(t, events)
	if len(evt.Questions) != 1 {
		t.Fatalf("expected one question, got %d", len(evt.Questions))
	}
	q := evt.Questions[0]
	for _, want := range []string{"Bash", "git push", "needs context", "push the feature branch", "the user asked me to ship"} {
		if !strings.Contains(q.Question, want) {
			t.Fatalf("question %q is missing %q", q.Question, want)
		}
	}
	if len(q.Options) != 2 || q.Options[0].Label != "Approve" || q.Options[1].Label != "Deny" {
		t.Fatalf("options = %+v, want Approve then Deny", q.Options)
	}

	evt.ResponseChan <- []agentdomain.UserQuestionAnswer{{Header: "Approval", SelectedLabels: []string{"Approve"}}}

	out := <-outcome
	if out.err != nil {
		t.Fatalf("Escalate: %v", out.err)
	}
	if out.res.Status != agentdomain.EscalationApproved || !out.res.Approved {
		t.Fatalf("result = %+v, want approved", out.res)
	}
	if !svc.escalations.takeBypass("Bash", `{"command":"git push"}`) {
		t.Fatal("approval must arm the bypass marker")
	}
	if svc.escalations.takeBypass("Bash", `{"command":"git push"}`) {
		t.Fatal("the bypass marker is one-shot")
	}
	if _, ok := svc.escalations.claim("Bash", `{"command":"git push"}`); ok {
		t.Fatal("the escalation attempt must be consumed")
	}
}

func TestApprovalEscalatorDeniedCarriesAnswer(t *testing.T) {
	svc := &AgentServiceImpl{escalations: newJudgeEscalations()}
	svc.escalations.record("Bash", `{"command":"git push"}`, "needs context")

	events := make(chan agentdomain.ChatEvent, 8)
	pub := &eventPublisher{requestID: "req-1", chatEvents: events}
	esc := &approvalEscalator{svc: svc, publisher: pub}

	outcome := escalateAsync(t, esc, agentdomain.ApprovalEscalationRequest{ToolCall: escalationToolCall("tc-1"), What: "push", Why: "ship"})
	evt := awaitQuestion(t, events)
	evt.ResponseChan <- []agentdomain.UserQuestionAnswer{{Header: "Approval", OtherText: "not yet, run the e2e suite first"}}

	out := <-outcome
	if out.err != nil {
		t.Fatalf("Escalate: %v", out.err)
	}
	if out.res.Status != agentdomain.EscalationDenied || out.res.Approved {
		t.Fatalf("result = %+v, want denied", out.res)
	}
	if out.res.Answer != "not yet, run the e2e suite first" {
		t.Fatalf("answer = %q, want the user's free text", out.res.Answer)
	}
	if svc.escalations.takeBypass("Bash", `{"command":"git push"}`) {
		t.Fatal("a denial must not arm the bypass marker")
	}
}

func TestApprovalEscalatorDismissedCountsAsDenied(t *testing.T) {
	svc := &AgentServiceImpl{escalations: newJudgeEscalations()}
	svc.escalations.record("Bash", `{"command":"git push"}`, "needs context")

	events := make(chan agentdomain.ChatEvent, 8)
	pub := &eventPublisher{requestID: "req-1", chatEvents: events}
	esc := &approvalEscalator{svc: svc, publisher: pub}

	outcome := escalateAsync(t, esc, agentdomain.ApprovalEscalationRequest{ToolCall: escalationToolCall("tc-1"), What: "push", Why: "ship"})
	evt := awaitQuestion(t, events)
	close(evt.ResponseChan)

	out := <-outcome
	if out.err != nil {
		t.Fatalf("Escalate: %v", out.err)
	}
	if out.res.Status != agentdomain.EscalationDenied || out.res.Answer != "" {
		t.Fatalf("result = %+v, want denied without an answer", out.res)
	}
	if _, ok := svc.escalations.claim("Bash", `{"command":"git push"}`); ok {
		t.Fatal("a dismissed prompt must consume the one escalation attempt")
	}
}

func TestApprovalEscalatorRequiresJudgeRejection(t *testing.T) {
	svc := &AgentServiceImpl{escalations: newJudgeEscalations()}
	esc := &approvalEscalator{svc: svc, publisher: &eventPublisher{requestID: "req-1", chatEvents: make(chan agentdomain.ChatEvent, 1)}}

	res, err := esc.Escalate(context.Background(), agentdomain.ApprovalEscalationRequest{ToolCall: escalationToolCall("tc-1"), What: "push", Why: "ship"})
	if err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	if res.Status != agentdomain.EscalationNotRejected {
		t.Fatalf("status = %q, want not_rejected", res.Status)
	}
	if len(svc.escalations.byKey) != 0 {
		t.Fatal("an ineligible call must not be tracked")
	}
}

func TestApprovalEscalatorOneAttemptPerRejectedCall(t *testing.T) {
	svc := &AgentServiceImpl{escalations: newJudgeEscalations()}
	svc.escalations.record("Bash", `{"command":"git push"}`, "needs context")
	svc.escalations.claim("Bash", `{"command":"git push"}`)

	esc := &approvalEscalator{svc: svc, publisher: &eventPublisher{requestID: "req-1", chatEvents: make(chan agentdomain.ChatEvent, 1)}}
	res, err := esc.Escalate(context.Background(), agentdomain.ApprovalEscalationRequest{ToolCall: escalationToolCall("tc-1"), What: "push", Why: "ship"})
	if err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	if res.Status != agentdomain.EscalationAlreadyAsked || res.JudgeReason != "needs context" {
		t.Fatalf("result = %+v, want already_asked with the original reason", res)
	}
}

func TestApprovalEscalatorCancelled(t *testing.T) {
	svc := &AgentServiceImpl{escalations: newJudgeEscalations()}
	svc.escalations.record("Bash", `{"command":"git push"}`, "needs context")

	esc := &approvalEscalator{svc: svc, publisher: &eventPublisher{requestID: "req-1", chatEvents: make(chan agentdomain.ChatEvent, 1)}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := esc.Escalate(ctx, agentdomain.ApprovalEscalationRequest{ToolCall: escalationToolCall("tc-1"), What: "push", Why: "ship"}); err == nil {
		t.Fatal("a cancelled session must abort the escalation with an error")
	}
}

func TestRequestJudgeApprovalRecordsRejectionAndHints(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.Model = "test/model"
	svc := &AgentServiceImpl{
		config:           cfg,
		conversationRepo: &conversationmocks.FakeConversationRepository{},
		judge:            stubJudgeApprover{verdict: agentdomain.JudgeVerdict{Decision: agentdomain.JudgeDecisionRejected, Reason: "curl was not requested"}},
		escalations:      newJudgeEscalations(),
	}
	events := make(chan agentdomain.ChatEvent, 8)
	pub := &eventPublisher{requestID: "req-1", chatEvents: events}

	approved, reason, err := svc.requestJudgeApproval(context.Background(), escalationToolCall("tc-1"), pub)
	if err != nil {
		t.Fatalf("requestJudgeApproval: %v", err)
	}
	if approved {
		t.Fatal("a judge rejection must not be approved")
	}
	if !strings.HasPrefix(reason, "test/model: curl was not requested") {
		t.Fatalf("reason %q must name the judge model and verdict", reason)
	}
	if !strings.Contains(reason, "RequestApproval") {
		t.Fatalf("rejection reason %q must hint at the escalation path", reason)
	}

	select {
	case e := <-events:
		je, ok := e.(agentdomain.JudgeVerdictChatEvent)
		if !ok || je.Decision != agentdomain.JudgeDecisionRejected {
			t.Fatalf("expected a rejected JudgeVerdictChatEvent, got %#v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no judge verdict event was published")
	}

	esc, ok := svc.escalations.state("Bash", `{"command":"git push"}`)
	if !ok {
		t.Fatal("the rejection must be tracked for escalation")
	}
	if esc.reason != "curl was not requested" || esc.asked {
		t.Fatalf("tracked escalation = %+v, want the raw judge reason without consuming the attempt", esc)
	}
}

func TestRequestJudgeApprovalHonoursUserApprovalBypass(t *testing.T) {
	svc := &AgentServiceImpl{escalations: newJudgeEscalations()}
	svc.escalations.record("Bash", `{"command":"git push"}`, "needs context")
	svc.escalations.approve("Bash", `{"command":"git push"}`)

	approved, reason, err := svc.requestJudgeApproval(context.Background(), escalationToolCall("tc-1"), nil)
	if err != nil {
		t.Fatalf("requestJudgeApproval: %v", err)
	}
	if !approved || reason != "" {
		t.Fatalf("result = (%v, %q), want approved without a reason", approved, reason)
	}
	if svc.escalations.takeBypass("Bash", `{"command":"git push"}`) {
		t.Fatal("the bypass must be consumed by the approved invocation")
	}
}
