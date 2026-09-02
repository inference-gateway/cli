package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// fakeEscalationGate records the request it received and answers with a canned outcome.
type fakeEscalationGate struct {
	gotReq  agentdomain.ApprovalEscalationRequest
	outcome agentdomain.ApprovalEscalationResult
	err     error
}

func (f *fakeEscalationGate) Escalate(ctx context.Context, req agentdomain.ApprovalEscalationRequest) (agentdomain.ApprovalEscalationResult, error) {
	f.gotReq = req
	if f.err != nil {
		return agentdomain.ApprovalEscalationResult{}, f.err
	}
	return f.outcome, nil
}

// fakeModeManager pins the agent mode for IsEnabled tests.
type fakeModeManager struct {
	mode agentdomain.AgentMode
}

func (f *fakeModeManager) GetAgentMode() agentdomain.AgentMode     { return f.mode }
func (f *fakeModeManager) SetAgentMode(mode agentdomain.AgentMode) { f.mode = mode }
func (f *fakeModeManager) CycleAgentMode() agentdomain.AgentMode   { return f.mode }

func escalationArgs() map[string]any {
	return map[string]any{
		"tool":      "Bash",
		"arguments": map[string]any{"command": "git push"},
		"what":      "push the feature branch",
		"why":       "the user asked me to ship it",
	}
}

func executeEscalation(t *testing.T, gate agentdomain.ApprovalEscalation, args map[string]any) *agentdomain.ToolExecutionResult {
	t.Helper()
	tool := NewRequestApprovalTool(config.DefaultConfig(), nil)
	ctx := context.Background()
	if gate != nil {
		ctx = agentdomain.WithApprovalEscalation(ctx, gate)
	}
	result, err := tool.Execute(ctx, args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result == nil {
		t.Fatal("Execute returned no result")
	}
	return result
}

func resultData(t *testing.T, result *agentdomain.ToolExecutionResult) map[string]any {
	t.Helper()
	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", result.Data)
	}
	return data
}

func TestRequestApprovalWithoutGateDegrades(t *testing.T) {
	result := executeEscalation(t, nil, escalationArgs())

	if !result.Success {
		t.Fatalf("the no-approver degrade must not be a failure, got error %q", result.Error)
	}
	data := resultData(t, result)
	if data["available"] != false {
		t.Fatalf("available = %v, want false", data["available"])
	}
	if msg, _ := data["message"].(string); !strings.Contains(msg, "No interactive approver") {
		t.Fatalf("message %q must be distinguishable about the missing approver", msg)
	}
	if resultStatus(result) != agentdomain.EscalationNoApprover {
		t.Fatalf("status = %q, want no_approver", resultStatus(result))
	}
}

func TestRequestApprovalApprovedResult(t *testing.T) {
	gate := &fakeEscalationGate{outcome: agentdomain.ApprovalEscalationResult{
		Status: agentdomain.EscalationApproved, Approved: true, JudgeReason: "needs context",
	}}
	result := executeEscalation(t, gate, escalationArgs())

	if !result.Success {
		t.Fatalf("approved escalation must succeed, got error %q", result.Error)
	}
	data := resultData(t, result)
	if data["approved"] != true || data["status"] != agentdomain.EscalationApproved {
		t.Fatalf("data = %v, want approved escalation", data)
	}
	if msg, _ := data["message"].(string); !strings.Contains(msg, "APPROVED") || !strings.Contains(msg, "Bash") {
		t.Fatalf("message %q must tell the agent to re-issue the call", msg)
	}
	if gate.gotReq.ToolCall.Function.Name != "Bash" {
		t.Fatalf("escalated tool = %q, want Bash", gate.gotReq.ToolCall.Function.Name)
	}
	if gate.gotReq.ToolCall.Function.Arguments != `{"command":"git push"}` {
		t.Fatalf("arguments = %q, want the canonical JSON", gate.gotReq.ToolCall.Function.Arguments)
	}
	if gate.gotReq.What != "push the feature branch" || gate.gotReq.Why != "the user asked me to ship it" {
		t.Fatalf("what/why = (%q, %q), want the agent's own words", gate.gotReq.What, gate.gotReq.Why)
	}
}

func TestRequestApprovalDeniedCarriesAnswer(t *testing.T) {
	gate := &fakeEscalationGate{outcome: agentdomain.ApprovalEscalationResult{
		Status: agentdomain.EscalationDenied, Answer: "not yet, run the e2e suite first", JudgeReason: "needs context",
	}}
	result := executeEscalation(t, gate, escalationArgs())

	if !result.Success {
		t.Fatalf("a denial is an answered escalation, not a failure: %q", result.Error)
	}
	data := resultData(t, result)
	if data["approved"] != false {
		t.Fatalf("approved = %v, want false", data["approved"])
	}
	if data["answer"] != "not yet, run the e2e suite first" {
		t.Fatalf("answer = %v, want the user's text", data["answer"])
	}
	if msg, _ := data["message"].(string); !strings.Contains(msg, "not yet, run the e2e suite first") {
		t.Fatalf("message %q must carry the user's answer to the agent", msg)
	}
}

func TestRequestApprovalIneligibleCallsFail(t *testing.T) {
	tests := []struct {
		name        string
		outcome     agentdomain.ApprovalEscalationResult
		wantErrPart string
	}{
		{
			name:        "call was not judge-rejected",
			outcome:     agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationNotRejected},
			wantErrPart: "only escalates calls the judge rejected",
		},
		{
			name:        "already escalated once",
			outcome:     agentdomain.ApprovalEscalationResult{Status: agentdomain.EscalationAlreadyAsked},
			wantErrPart: "already escalated once",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := executeEscalation(t, &fakeEscalationGate{outcome: tt.outcome}, escalationArgs())
			if result.Success {
				t.Fatalf("%s must fail, got data %v", tt.name, result.Data)
			}
			if !strings.Contains(result.Error, tt.wantErrPart) {
				t.Fatalf("error %q must contain %q", result.Error, tt.wantErrPart)
			}
		})
	}
}

func TestRequestApprovalGateFailure(t *testing.T) {
	gate := &fakeEscalationGate{err: errors.New("boom")}
	result := executeEscalation(t, gate, escalationArgs())

	if result.Success {
		t.Fatal("a failed escalation must not report success")
	}
	if !strings.Contains(result.Error, "boom") {
		t.Fatalf("error %q must surface the gate failure", result.Error)
	}
}

func TestRequestApprovalValidate(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{name: "missing tool", args: map[string]any{"arguments": map[string]any{}, "what": "w", "why": "y"}, wantErr: "tool parameter"},
		{name: "non-string tool", args: map[string]any{"tool": 42, "arguments": map[string]any{}, "what": "w", "why": "y"}, wantErr: "tool parameter"},
		{name: "missing arguments", args: map[string]any{"tool": "Bash", "what": "w", "why": "y"}, wantErr: "arguments parameter"},
		{name: "empty what", args: map[string]any{"tool": "Bash", "arguments": map[string]any{}, "what": "  ", "why": "y"}, wantErr: "what parameter"},
		{name: "missing why", args: map[string]any{"tool": "Bash", "arguments": map[string]any{}, "what": "w"}, wantErr: "why parameter"},
		{name: "valid empty arguments", args: map[string]any{"tool": "Bash", "arguments": map[string]any{}, "what": "w", "why": "y"}},
		{name: "valid trimmed fields", args: map[string]any{"tool": " Bash ", "arguments": map[string]any{"command": "git push"}, "what": " push ", "why": " ship "}},
	}
	tool := NewRequestApprovalTool(config.DefaultConfig(), nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: unexpected error %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestRequestApprovalIsEnabled(t *testing.T) {
	tests := []struct {
		name        string
		behaviour   string
		mode        agentdomain.AgentMode
		wantEnabled bool
	}{
		{name: "default config, standard mode", behaviour: "", mode: agentdomain.AgentModeStandard},
		{name: "judge behaviour in config", behaviour: config.ApprovalBehaviourJudge, mode: agentdomain.AgentModeStandard, wantEnabled: true},
		{name: "runtime auto-with-judge mode", behaviour: "", mode: agentdomain.AgentModeAutoWithJudge, wantEnabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Tools.Safety.ApprovalBehaviour = tt.behaviour
			tool := NewRequestApprovalTool(cfg, &fakeModeManager{mode: tt.mode})
			if got := tool.IsEnabled(); got != tt.wantEnabled {
				t.Fatalf("IsEnabled = %v, want %v", got, tt.wantEnabled)
			}
		})
	}
}

func TestRequestApprovalFormats(t *testing.T) {
	tool := NewRequestApprovalTool(config.DefaultConfig(), nil)
	approved := executeEscalation(t, &fakeEscalationGate{outcome: agentdomain.ApprovalEscalationResult{
		Status: agentdomain.EscalationApproved, Approved: true,
	}}, escalationArgs())
	denied := executeEscalation(t, &fakeEscalationGate{outcome: agentdomain.ApprovalEscalationResult{
		Status: agentdomain.EscalationDenied, Answer: "run tests first",
	}}, escalationArgs())
	noApprover := executeEscalation(t, nil, escalationArgs())
	failed := executeEscalation(t, &fakeEscalationGate{outcome: agentdomain.ApprovalEscalationResult{
		Status: agentdomain.EscalationNotRejected,
	}}, escalationArgs())

	if got := tool.FormatPreview(approved); got != "Approved by user" {
		t.Fatalf("approved preview = %q", got)
	}
	if got := tool.FormatPreview(denied); got != "Denied by user" {
		t.Fatalf("denied preview = %q", got)
	}
	if got := tool.FormatPreview(noApprover); got != "No approver reachable" {
		t.Fatalf("no-approver preview = %q", got)
	}
	if got := tool.FormatPreview(failed); got != "Escalation not possible" {
		t.Fatalf("failed preview = %q", got)
	}
	if got := tool.FormatForLLM(approved); !strings.Contains(got, "APPROVED") {
		t.Fatalf("approved LLM format = %q", got)
	}
	if got := tool.FormatForLLM(failed); !strings.Contains(got, "only escalates calls the judge rejected") {
		t.Fatalf("failed LLM format = %q", got)
	}
	if got := tool.FormatResult(approved, agentdomain.FormatterUI); !strings.Contains(got, "RequestApproval(") {
		t.Fatalf("UI format = %q", got)
	}
}
