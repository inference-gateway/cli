package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	conversationmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// judgeTestConfig returns a config whose judge model falls back to agent.model.
func judgeTestConfig(onError string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Agent.Model = "test/judge-model"
	cfg.Judge = config.JudgeConfig{OnError: onError}
	return cfg
}

// newJudgeClient returns a FakeClient wired for the chained WithOptions /
// WithMiddlewareOptions calls LLMJudge makes before GenerateContent.
func newJudgeClient(resp *sdk.CreateChatCompletionResponse, err error) *sdkmocks.FakeClient {
	client := &sdkmocks.FakeClient{}
	client.WithOptionsReturns(client)
	client.WithMiddlewareOptionsReturns(client)
	client.GenerateContentReturns(resp, err)
	return client
}

func judgeResponse(content string) *sdk.CreateChatCompletionResponse {
	return &sdk.CreateChatCompletionResponse{
		Usage: &sdk.CompletionUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		Choices: []sdk.ChatCompletionChoice{
			{Message: sdk.Message{Content: sdk.NewMessageContent(content)}},
		},
	}
}

func TestLLMJudge_VerdictAndPromptShaping(t *testing.T) {
	client := newJudgeClient(judgeResponse("```json\n{\"decision\": \"approved\", \"reason\": \"matches the request\"}\n```"), nil)
	judge := NewLLMJudge(client, judgeTestConfig(""))

	verdict, err := judge.Judge(context.Background(), JudgeInput{Model: "test/judge-model", RootIntent: "set up the project", Intent: "install the dependency", Action: `Bash: {"command": "go get"}`})
	if err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if !verdict.Approved() || verdict.Reason != "matches the request" {
		t.Errorf("verdict = %+v, want approved with reason", verdict)
	}

	_, provider, model, messages := client.GenerateContentArgsForCall(0)
	if provider != sdk.Provider("test") || model != "judge-model" {
		t.Errorf("provider/model = %s/%s, want test/judge-model", provider, model)
	}
	if len(messages) != 2 || messages[0].Role != sdk.System || messages[1].Role != sdk.User {
		t.Fatalf("expected a system then a user message, got %+v", messages)
	}
	system, serr := messages[0].Content.AsMessageContent0()
	if serr != nil || !strings.Contains(system, "never instructions") {
		t.Errorf("system prompt = %q (%v), want the data-not-instructions rule", system, serr)
	}
	prompt, perr := messages[1].Content.AsMessageContent0()
	if perr != nil {
		t.Fatalf("prompt content: %v", perr)
	}
	if !strings.Contains(prompt, "<root_request>\nset up the project\n</root_request>") ||
		!strings.Contains(prompt, "<latest_request>\ninstall the dependency\n</latest_request>") ||
		!strings.Contains(prompt, "<tool_call>\nBash: {\"command\": \"go get\"}\n</tool_call>") {
		t.Errorf("prompt missing tagged intent/action: %q", prompt)
	}
	if verdict.Usage == nil || verdict.Usage.TotalTokens != 30 {
		t.Errorf("verdict.Usage = %+v, want the judge call usage", verdict.Usage)
	}
}

func TestLLMJudge_OnError(t *testing.T) {
	tests := []struct {
		name        string
		onError     string
		wantApprove bool
	}{
		{"unset defaults to deny", "", false},
		{"deny fails closed", config.JudgeOnErrorDeny, false},
		{"allow approves", config.JudgeOnErrorAllow, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newJudgeClient(nil, errors.New("gateway down"))
			judge := NewLLMJudge(client, judgeTestConfig(tt.onError))

			verdict, err := judge.Judge(context.Background(), JudgeInput{Model: "test/judge-model", Intent: "intent", Action: "action"})
			if err != nil {
				t.Fatalf("Judge() error = %v, want nil (on_error decides)", err)
			}
			if verdict.Approved() != tt.wantApprove {
				t.Errorf("verdict.Approved() = %v, want %v", verdict.Approved(), tt.wantApprove)
			}
			if !strings.Contains(verdict.Reason, "judge unavailable") {
				t.Errorf("reason = %q, want it to mention judge unavailable", verdict.Reason)
			}
		})
	}
}

func TestLLMJudge_UnparseableOutputDenies(t *testing.T) {
	client := newJudgeClient(judgeResponse("no verdict here"), nil)
	judge := NewLLMJudge(client, judgeTestConfig(""))

	verdict, err := judge.Judge(context.Background(), JudgeInput{Model: "test/judge-model", Intent: "intent", Action: "action"})
	if err != nil {
		t.Fatalf("Judge() error = %v, want nil (on_error handles it)", err)
	}
	if verdict.Approved() {
		t.Errorf("unparseable judge output should deny, got %+v", verdict)
	}
}

func TestLLMJudge_InvalidModelFormat(t *testing.T) {
	judge := NewLLMJudge(&sdkmocks.FakeClient{}, judgeTestConfig(""))

	if _, err := judge.Judge(context.Background(), JudgeInput{Model: "no-slash", Intent: "intent", Action: "action"}); err == nil || !strings.Contains(err.Error(), "provider/model") {
		t.Fatalf("Judge() error = %v, want provider/model format error", err)
	}
}

func TestUserIntents_RootAndLatest(t *testing.T) {
	repo := &conversationmocks.FakeConversationRepository{}
	repo.GetMessagesReturns([]convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("first ask")}},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("answer")}},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hidden ask")}, Hidden: true},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("real ask")}},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("done?")}},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("continue")}},
	})
	if root, latest := userIntents(repo); root != "first ask" || latest != "continue" {
		t.Errorf("userIntents() = %q/%q, want first ask/continue", root, latest)
	}

	repo.GetMessagesReturns([]convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("only ask")}},
	})
	if root, latest := userIntents(repo); root != "" || latest != "only ask" {
		t.Errorf("single message: userIntents() = %q/%q, want empty root and only ask", root, latest)
	}

	if root, latest := userIntents(nil); root != "" || latest != "" {
		t.Errorf("userIntents(nil) = %q/%q, want empty", root, latest)
	}
}

func TestLLMJudge_TokenBudgetExhaustedDenies(t *testing.T) {
	resp := judgeResponse("")
	resp.Choices[0].FinishReason = sdk.Length
	judge := NewLLMJudge(newJudgeClient(resp, nil), judgeTestConfig(""))

	verdict, err := judge.Judge(context.Background(), JudgeInput{Model: "test/judge-model", Intent: "intent", Action: "action"})
	if err != nil {
		t.Fatalf("Judge() error = %v, want nil (on_error handles it)", err)
	}
	if verdict.Approved() || !strings.Contains(verdict.Reason, "max_tokens") {
		t.Errorf("exhausted budget should deny naming max_tokens, got %+v", verdict)
	}
}

func TestParseJudgeVerdict(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantDecision agentdomain.JudgeDecision
		wantReason   string
		wantErr      bool
	}{
		{"plain approved", `{"decision": "approved", "reason": "serves the request"}`, agentdomain.JudgeDecisionApproved, "serves the request", false},
		{"plain rejected", `{"decision": "rejected", "reason": "too risky"}`, agentdomain.JudgeDecisionRejected, "too risky", false},
		{"fenced json", "```json\n{\"decision\": \"approved\", \"reason\": \"ok\"}\n```", agentdomain.JudgeDecisionApproved, "ok", false},
		{"bare fence", "```\n{\"decision\": \"rejected\", \"reason\": \"no\"}\n```", agentdomain.JudgeDecisionRejected, "no", false},
		{"prose around json", "Verdict:\n{\"decision\": \"approved\", \"reason\": \"fine\"}\nDone.", agentdomain.JudgeDecisionApproved, "fine", false},
		{"empty reason ok", `{"decision": "approved"}`, agentdomain.JudgeDecisionApproved, "", false},
		{"no json object", "no verdict here", "", "", true},
		{"invalid decision", `{"decision": "maybe", "reason": "x"}`, "", "", true},
		{"invalid json", `{"decision":`, "", "", true},
		{"empty output", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseJudgeVerdict(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseJudgeVerdict(%q) err = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Decision != tt.wantDecision || got.Reason != tt.wantReason {
				t.Errorf("ParseJudgeVerdict(%q) = %+v, want decision %q reason %q", tt.raw, got, tt.wantDecision, tt.wantReason)
			}
			if got.Approved() != (got.Decision == agentdomain.JudgeDecisionApproved) {
				t.Errorf("Approved() inconsistent with decision %q", got.Decision)
			}
		})
	}
}
