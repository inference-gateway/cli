package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	conversationmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// rejectingJudge returns a fake judge that rejects every call with reason.
func rejectingJudge(reason string) *agentdomainmocks.FakeJudgeApprover {
	judge := &agentdomainmocks.FakeJudgeApprover{}
	judge.JudgeStub = func(_ context.Context, in agentdomain.JudgeInput) (agentdomain.JudgeVerdicts, error) {
		out := agentdomain.JudgeVerdicts{}
		for _, action := range in.Actions {
			out.Verdicts = append(out.Verdicts, agentdomain.JudgeVerdict{ID: action.ID, Decision: agentdomain.JudgeDecisionRejected, Reason: reason})
		}
		return out, nil
	}
	return judge
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

	if _, status := j.claim("Bash", `{"command":"git push"}`); status != agentdomain.EscalationNotRejected {
		t.Fatalf("claim before any rejection = %q, want not_rejected", status)
	}

	j.record("Bash", `{"command":"git push"}`, "needs context")
	j.record("Bash", `{"command":"git push"}`, "second rejection")
	if reason, status := j.claim("Bash", `{"command":  "git push"}`); status != "" || reason != "needs context" {
		t.Fatalf("claim = (%q, %q), want the first judge reason via the reformatted key", reason, status)
	}
	if reason, status := j.claim("Bash", `{"command":"git push"}`); status != agentdomain.EscalationAlreadyAsked || reason != "needs context" {
		t.Fatalf("second claim = (%q, %q), want already_asked with the reason", reason, status)
	}

	j.approve("Bash", `{"command":"git push"}`)
	if !j.takeBypass("Bash", `{"command": "git push"}`) {
		t.Fatal("an approved escalation must arm the bypass marker")
	}
	if j.takeBypass("Bash", `{"command":"git push"}`) {
		t.Fatal("the bypass marker must be consumed on first use")
	}
	j.record("Bash", `{"command":"git push"}`, "rejected again later")
	if reason, status := j.claim("Bash", `{"command":"git push"}`); status != "" || reason != "rejected again later" {
		t.Fatalf("claim after a consumed bypass = (%q, %q), want a fresh escalation", reason, status)
	}

	if _, status := j.claim("Read", "{}"); status != agentdomain.EscalationNotRejected {
		t.Fatalf("claim of an untracked call = %q, want not_rejected", status)
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

// awaitApproval waits for the published approval prompt and fails the test on timeout.
func awaitApproval(t *testing.T, events <-chan agentdomain.ChatEvent) agentdomain.ToolApprovalRequestedEvent {
	t.Helper()
	select {
	case e := <-events:
		evt, ok := e.(agentdomain.ToolApprovalRequestedEvent)
		if !ok {
			t.Fatalf("expected ToolApprovalRequestedEvent, got %T", e)
		}
		return evt
	case <-time.After(2 * time.Second):
		t.Fatal("no approval prompt was published")
		return agentdomain.ToolApprovalRequestedEvent{}
	}
}

// escalationService returns an agent service with one tracked judge rejection.
func escalationService() *Agent {
	svc := &Agent{escalations: newJudgeEscalations(), conversationRepo: &conversationmocks.FakeConversationRepository{}}
	svc.escalations.record("Bash", `{"command":"git push"}`, "needs context")
	return svc
}

func TestApprovalEscalatorApprovedSetsBypass(t *testing.T) {
	svc := escalationService()
	events := make(chan agentdomain.ChatEvent, 8)
	pub := &eventPublisher{requestID: "req-1", chatEvents: events}
	esc := &approvalEscalator{svc: svc, publisher: pub}

	outcome := escalateAsync(t, esc, agentdomain.ApprovalEscalationRequest{
		ToolCall: escalationToolCall(""),
		What:     "push the feature branch",
		Why:      "the user asked me to ship it",
	})

	evt := awaitApproval(t, events)
	if evt.ToolCall.Function.Name != "Bash" || evt.ToolCall.Function.Arguments != `{"command":"git push"}` {
		t.Fatalf("approval prompt is for %s(%s), want the rejected Bash call", evt.ToolCall.Function.Name, evt.ToolCall.Function.Arguments)
	}
	if evt.ToolCall.ID == "" {
		t.Fatal("the escalated call must carry its own ID for the pending entry")
	}
	for _, want := range []string{"needs context", "push the feature branch", "the user asked me to ship"} {
		if !strings.Contains(evt.Context, want) {
			t.Fatalf("approval context %q is missing %q", evt.Context, want)
		}
	}

	evt.ResponseChan <- agentdomain.ApprovalApprove

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
}

func TestApprovalEscalatorRejectedDoesNotArmBypass(t *testing.T) {
	svc := escalationService()
	events := make(chan agentdomain.ChatEvent, 8)
	esc := &approvalEscalator{svc: svc, publisher: &eventPublisher{requestID: "req-1", chatEvents: events}}

	outcome := escalateAsync(t, esc, agentdomain.ApprovalEscalationRequest{ToolCall: escalationToolCall(""), What: "push", Why: "ship"})
	evt := awaitApproval(t, events)
	evt.ResponseChan <- agentdomain.ApprovalReject

	out := <-outcome
	if out.err != nil {
		t.Fatalf("Escalate: %v", out.err)
	}
	if out.res.Status != agentdomain.EscalationDenied || out.res.Approved || out.res.JudgeReason != "needs context" {
		t.Fatalf("result = %+v, want denied with the judge reason", out.res)
	}
	if svc.escalations.takeBypass("Bash", `{"command":"git push"}`) {
		t.Fatal("a rejection must not arm the bypass marker")
	}
	if _, status := svc.escalations.claim("Bash", `{"command":"git push"}`); status == "" {
		t.Fatal("a rejection must consume the one escalation attempt")
	}
}

func TestApprovalEscalatorDismissedCountsAsDenied(t *testing.T) {
	svc := escalationService()
	events := make(chan agentdomain.ChatEvent, 8)
	esc := &approvalEscalator{svc: svc, publisher: &eventPublisher{requestID: "req-1", chatEvents: events}}

	outcome := escalateAsync(t, esc, agentdomain.ApprovalEscalationRequest{ToolCall: escalationToolCall(""), What: "push", Why: "ship"})
	evt := awaitApproval(t, events)
	close(evt.ResponseChan)

	out := <-outcome
	if out.err != nil {
		t.Fatalf("Escalate: %v", out.err)
	}
	if out.res.Status != agentdomain.EscalationDenied || out.res.Approved {
		t.Fatalf("result = %+v, want denied", out.res)
	}
	if svc.escalations.takeBypass("Bash", `{"command":"git push"}`) {
		t.Fatal("a dismissed prompt must not arm the bypass marker")
	}
}

func TestApprovalEscalatorRequiresJudgeRejection(t *testing.T) {
	svc := &Agent{escalations: newJudgeEscalations()}
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
	svc := &Agent{escalations: newJudgeEscalations()}
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
	esc := &approvalEscalator{svc: escalationService(), publisher: &eventPublisher{requestID: "req-1", chatEvents: make(chan agentdomain.ChatEvent, 1)}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := esc.Escalate(ctx, agentdomain.ApprovalEscalationRequest{ToolCall: escalationToolCall("tc-1"), What: "push", Why: "ship"}); err == nil {
		t.Fatal("a cancelled session must abort the escalation with an error")
	}
}

func TestRequestJudgeApprovalRecordsRejectionAndHints(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.Model = "test/model"
	svc := &Agent{
		config:           cfg,
		conversationRepo: &conversationmocks.FakeConversationRepository{},
		judge:            rejectingJudge("curl was not requested"),
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

	if reason, status := svc.escalations.claim("Bash", `{"command":"git push"}`); status != "" || reason != "curl was not requested" {
		t.Fatalf("tracked escalation = (%q, %q), want the raw judge reason with the attempt still available", reason, status)
	}
}

func TestRequestJudgeApprovalHonoursUserApprovalBypass(t *testing.T) {
	svc := &Agent{escalations: newJudgeEscalations()}
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

func TestDecideWithJudgeBatchesCallsAndSkipsBypassed(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agent.Model = "test/model"
	judge := rejectingJudge("not requested")
	svc := &Agent{
		config:           cfg,
		conversationRepo: &conversationmocks.FakeConversationRepository{},
		judge:            judge,
		escalations:      newJudgeEscalations(),
	}
	svc.escalations.record("Bash", `{"command":"git push"}`, "needs context")
	svc.escalations.approve("Bash", `{"command":"git push"}`)
	events := make(chan agentdomain.ChatEvent, 8)
	pub := &eventPublisher{requestID: "req-1", chatEvents: events}

	bypassed := escalationToolCall("tc-1")
	curl := sdk.ChatCompletionMessageToolCall{ID: "tc-2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"curl x"}`}}
	write := sdk.ChatCompletionMessageToolCall{ID: "tc-3", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Write", Arguments: `{"path":"a"}`}}

	decisions := svc.decideWithJudge(context.Background(), []sdk.ChatCompletionMessageToolCall{bypassed, curl, write}, pub)

	if judge.JudgeCallCount() != 1 {
		t.Fatalf("judge calls = %d, want one batched request", judge.JudgeCallCount())
	}
	_, in := judge.JudgeArgsForCall(0)
	if len(in.Actions) != 2 || in.Actions[0].ID != "tc-2" || in.Actions[1].ID != "tc-3" {
		t.Fatalf("judge actions = %+v, want only the two non-bypassed calls", in.Actions)
	}
	if d := decisions["tc-1"]; !d.Approved || d.Reason != "" {
		t.Fatalf("bypassed call = %+v, want approved without a reason", d)
	}
	for _, id := range []string{"tc-2", "tc-3"} {
		if d := decisions[id]; d.Approved || !strings.HasPrefix(d.Reason, "test/model: not requested") {
			t.Fatalf("decision for %s = %+v, want judge rejection", id, d)
		}
	}
	if len(events) != 2 {
		t.Fatalf("published %d verdict events, want one per judged call", len(events))
	}
}
