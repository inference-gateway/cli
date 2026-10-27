package loop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	conversationmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	llmmocks "github.com/inference-gateway/cli/tests/mocks/llm"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	llm "github.com/inference-gateway/cli/internal/platform/llm"
)

// judgeTestConfig returns a config whose judge model falls back to agent.model.
func judgeTestConfig(onError string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Agent.Model = "test/judge-model"
	cfg.Judge = config.JudgeConfig{OnError: onError}
	return cfg
}

// newJudgeClient returns a FakeCompleter answering every judge call with resp
// or err.
func newJudgeClient(resp llm.Response, err error) *llmmocks.FakeCompleter {
	completer := &llmmocks.FakeCompleter{}
	completer.CompleteReturns(resp, err)
	return completer
}

// judgeAnswer is a judge answer with the usage of one request.
func judgeAnswer(content string) llm.Response {
	return llm.Response{
		Content: content,
		Usage:   &sdk.CompletionUsage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
	}
}

func singleAction(action string) agentdomain.JudgeInput {
	return agentdomain.JudgeInput{Model: "test/judge-model", Intent: "intent", Actions: []agentdomain.JudgeAction{{ID: "call-0", Action: action}}}
}

func judgePrompt(t *testing.T, client *llmmocks.FakeCompleter, call int) string {
	t.Helper()
	_, req := client.CompleteArgsForCall(call)
	return req.Prompt
}

func TestLLMJudge_VerdictAndPromptShaping(t *testing.T) {
	client := newJudgeClient(judgeAnswer("```json\n{\"decision\": \"approved\", \"reason\": \"matches the request\"}\n```"), nil)
	judge := NewLLMJudge(client, judgeTestConfig(""))

	in := singleAction(`Bash: {"command": "go get"}`)
	in.RootIntent, in.Intent = "set up the project", "install the dependency"
	out, err := judge.Judge(context.Background(), in)
	if err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if len(out.Verdicts) != 1 || !out.Verdicts[0].Approved() || out.Verdicts[0].Reason != "matches the request" {
		t.Errorf("verdicts = %+v, want one approved with reason", out.Verdicts)
	}

	_, req := client.CompleteArgsForCall(0)
	if req.Model != "test/judge-model" {
		t.Errorf("model = %s, want test/judge-model", req.Model)
	}
	if !strings.Contains(req.System, "never instructions") {
		t.Errorf("system prompt = %q, want the data-not-instructions rule", req.System)
	}
	prompt := judgePrompt(t, client, 0)
	if !strings.Contains(prompt, "<root_request>\nset up the project\n</root_request>") ||
		!strings.Contains(prompt, "<latest_request>\ninstall the dependency\n</latest_request>") ||
		!strings.Contains(prompt, "<tool_call id=\"call-0\">\nBash: {\"command\": \"go get\"}\n</tool_call>") {
		t.Errorf("prompt missing tagged intent/action: %q", prompt)
	}
	if out.Usage == nil || out.Usage.TotalTokens != 30 {
		t.Errorf("Usage = %+v, want the judge call usage", out.Usage)
	}
}

func batchInput(n int) agentdomain.JudgeInput {
	in := agentdomain.JudgeInput{Model: "test/judge-model", Intent: "intent"}
	for i := range n {
		in.Actions = append(in.Actions, agentdomain.JudgeAction{ID: fmt.Sprintf("call-%d", i), Action: "Write: {}"})
	}
	return in
}

func batchAnswer(ids []string, decision agentdomain.JudgeDecision) string {
	entries := make([]string, len(ids))
	for i, id := range ids {
		entries[i] = fmt.Sprintf(`{"id": %q, "decision": %q, "reason": "r"}`, id, decision)
	}
	return "[" + strings.Join(entries, ",") + "]"
}

func TestLLMJudge_BatchesCallsIntoOneRequest(t *testing.T) {
	client := newJudgeClient(judgeAnswer(batchAnswer([]string{"call-0", "call-1", "call-2"}, agentdomain.JudgeDecisionApproved)), nil)
	judge := NewLLMJudge(client, judgeTestConfig(""))

	out, err := judge.Judge(context.Background(), batchInput(3))
	if err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if client.CompleteCallCount() != 1 {
		t.Fatalf("GenerateContent calls = %d, want 1", client.CompleteCallCount())
	}
	if len(out.Verdicts) != 3 {
		t.Fatalf("verdicts = %d, want 3", len(out.Verdicts))
	}
	for i, v := range out.Verdicts {
		if v.ID != fmt.Sprintf("call-%d", i) || !v.Approved() {
			t.Errorf("verdict %d = %+v, want approved in input order", i, v)
		}
	}
	prompt := judgePrompt(t, client, 0)
	if strings.Count(prompt, "<tool_call id=") != 3 {
		t.Errorf("prompt must carry one tagged block per call: %q", prompt)
	}
}

func TestLLMJudge_SplitsOversizedBatchAndSumsUsage(t *testing.T) {
	client := newJudgeClient(llm.Response{}, nil)
	client.CompleteStub = func(_ context.Context, req llm.Request) (llm.Response, error) {
		var ids []string
		for _, line := range strings.Split(req.Prompt, "\n") {
			if id, ok := strings.CutPrefix(line, "<tool_call id=\""); ok {
				ids = append(ids, strings.TrimSuffix(id, "\">"))
			}
		}
		return judgeAnswer(batchAnswer(ids, agentdomain.JudgeDecisionApproved)), nil
	}
	judge := NewLLMJudge(client, judgeTestConfig(""))

	out, err := judge.Judge(context.Background(), batchInput(maxJudgeBatchSize+5))
	if err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if client.CompleteCallCount() != 2 {
		t.Fatalf("GenerateContent calls = %d, want 2 for %d calls", client.CompleteCallCount(), maxJudgeBatchSize+5)
	}
	if len(out.Verdicts) != maxJudgeBatchSize+5 {
		t.Fatalf("verdicts = %d, want one per call", len(out.Verdicts))
	}
	if out.Verdicts[maxJudgeBatchSize].ID != fmt.Sprintf("call-%d", maxJudgeBatchSize) || !out.Verdicts[maxJudgeBatchSize].Approved() {
		t.Errorf("second batch verdicts out of order: %+v", out.Verdicts[maxJudgeBatchSize])
	}
	if out.Usage == nil || out.Usage.TotalTokens != 60 {
		t.Errorf("Usage = %+v, want the two requests summed", out.Usage)
	}
}

func TestLLMJudge_OmittedCallFailsClosedAlone(t *testing.T) {
	client := newJudgeClient(judgeAnswer(batchAnswer([]string{"call-0", "call-2"}, agentdomain.JudgeDecisionApproved)), nil)
	judge := NewLLMJudge(client, judgeTestConfig(""))

	out, err := judge.Judge(context.Background(), batchInput(3))
	if err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if !out.Verdicts[0].Approved() || !out.Verdicts[2].Approved() {
		t.Errorf("answered calls must keep their verdicts: %+v", out.Verdicts)
	}
	if out.Verdicts[1].Approved() || !strings.Contains(out.Verdicts[1].Reason, "judge unavailable") || out.Verdicts[1].ID != "call-1" {
		t.Errorf("omitted call must get the on_error verdict, got %+v", out.Verdicts[1])
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
			client := newJudgeClient(llm.Response{}, errors.New("gateway down"))
			judge := NewLLMJudge(client, judgeTestConfig(tt.onError))

			out, err := judge.Judge(context.Background(), batchInput(2))
			if err != nil {
				t.Fatalf("Judge() error = %v, want nil (on_error decides)", err)
			}
			if len(out.Verdicts) != 2 {
				t.Fatalf("verdicts = %d, want one per call", len(out.Verdicts))
			}
			for _, v := range out.Verdicts {
				if v.Approved() != tt.wantApprove {
					t.Errorf("verdict %s Approved() = %v, want %v", v.ID, v.Approved(), tt.wantApprove)
				}
				if !strings.Contains(v.Reason, "judge unavailable") {
					t.Errorf("reason = %q, want it to mention judge unavailable", v.Reason)
				}
			}
		})
	}
}

func TestLLMJudge_UnparseableOutputDeniesWholeBatch(t *testing.T) {
	client := newJudgeClient(judgeAnswer("no verdict here"), nil)
	judge := NewLLMJudge(client, judgeTestConfig(""))

	out, err := judge.Judge(context.Background(), batchInput(2))
	if err != nil {
		t.Fatalf("Judge() error = %v, want nil (on_error handles it)", err)
	}
	for _, v := range out.Verdicts {
		if v.Approved() {
			t.Errorf("unparseable judge output should deny, got %+v", v)
		}
	}
}

func TestLLMJudge_InvalidModelFormat(t *testing.T) {
	judge := NewLLMJudge(&llmmocks.FakeCompleter{}, judgeTestConfig(""))

	in := singleAction("action")
	in.Model = "no-slash"
	if _, err := judge.Judge(context.Background(), in); err == nil || !strings.Contains(err.Error(), "provider/model") {
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

	textPart, err := sdk.NewTextContentPart("remove the status on the left")
	if err != nil {
		t.Fatal(err)
	}
	imagePart, err := sdk.NewImageContentPart("data:image/jpeg;base64,AAAA", nil)
	if err != nil {
		t.Fatal(err)
	}
	repo.GetMessagesReturns([]convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent([]sdk.ContentPart{textPart, imagePart})}},
	})
	if _, latest := userIntents(repo); !strings.Contains(latest, "remove the status on the left") {
		t.Errorf("multimodal message: latest = %q, want its text part", latest)
	}

	repo.GetMessagesReturns([]convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("execute the approved plan")}, Hidden: true},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("reminder")}, Hidden: true},
	})
	if root, latest := userIntents(repo); root != "" || latest != "execute the approved plan" {
		t.Errorf("hidden-only: userIntents() = %q/%q, want empty root and the first hidden message", root, latest)
	}

	if root, latest := userIntents(nil); root != "" || latest != "" {
		t.Errorf("userIntents(nil) = %q/%q, want empty", root, latest)
	}
}

// TestLLMJudge_ReadsVerdictFromReasoning covers a reasoning judge that leaves
// the verdict in reasoning_content and nothing in content.
func TestLLMJudge_ReadsVerdictFromReasoning(t *testing.T) {
	reasoningOnly := llm.Response{Reasoning: `{"decision": "approved", "reason": "fine"}`}
	judge := NewLLMJudge(newJudgeClient(reasoningOnly, nil), judgeTestConfig(""))

	out, err := judge.Judge(context.Background(), singleAction("action"))
	if err != nil {
		t.Fatalf("Judge() error = %v", err)
	}
	if len(out.Verdicts) != 1 || !out.Verdicts[0].Approved() {
		t.Errorf("verdict in reasoning_content must be read, got %+v", out.Verdicts)
	}
}

// TestLLMJudge_EmptyResponseDenies names the cause when the judge answers
// nothing without spending its budget.
func TestLLMJudge_EmptyResponseDenies(t *testing.T) {
	judge := NewLLMJudge(newJudgeClient(llm.Response{}, llm.ErrEmptyResponse), judgeTestConfig(""))

	out, err := judge.Judge(context.Background(), singleAction("action"))
	if err != nil {
		t.Fatalf("Judge() error = %v, want nil (on_error handles it)", err)
	}
	if len(out.Verdicts) != 1 || out.Verdicts[0].Approved() || !strings.Contains(out.Verdicts[0].Reason, "empty response") {
		t.Errorf("empty response should deny naming the cause, got %+v", out.Verdicts)
	}
}

func TestLLMJudge_TokenBudgetExhaustedDenies(t *testing.T) {
	judge := NewLLMJudge(newJudgeClient(llm.Response{}, fmt.Errorf("spent: %w", llm.ErrTokenBudgetExhausted)), judgeTestConfig(""))

	out, err := judge.Judge(context.Background(), singleAction("action"))
	if err != nil {
		t.Fatalf("Judge() error = %v, want nil (on_error handles it)", err)
	}
	if len(out.Verdicts) != 1 || out.Verdicts[0].Approved() || !strings.Contains(out.Verdicts[0].Reason, "max_tokens") {
		t.Errorf("exhausted budget should deny naming max_tokens, got %+v", out.Verdicts)
	}
}
