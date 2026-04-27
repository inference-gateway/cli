package shortcuts

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"

	domain "github.com/inference-gateway/cli/internal/domain"
)

type stubTokenEstimator struct {
	estimate int
}

func (s *stubTokenEstimator) GetToolStats(domain.ToolService, domain.AgentMode) (int, int) {
	return 0, 0
}

func (s *stubTokenEstimator) EstimateMessagesTokens([]sdk.Message) int {
	return s.estimate
}

func TestContextShortcut_Execute_UsesProviderUsageWhenAvailable(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetSessionTokensReturns(domain.SessionTokenStats{
		LastInputTokens:   144_670,
		TotalInputTokens:  2_568_948,
		TotalOutputTokens: 32_242,
		TotalTokens:       2_601_190,
		RequestCount:      37,
	})
	repo.GetMessageCountReturns(113)

	model := &mockModelService{currentModel: "deepseek/deepseek-v4-flash"}
	tokenizer := &stubTokenEstimator{estimate: 999_999}

	sc := NewContextShortcut(repo, model, tokenizer)
	res, err := sc.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(res.Output, "**Current Context Size:** 144670 tokens") {
		t.Errorf("expected raw provider count, got: %s", res.Output)
	}
	if strings.Contains(res.Output, "(estimated)") {
		t.Errorf("did not expect (estimated) marker when provider returned usage, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "**Usage:** 14.5%") {
		t.Errorf("expected 14.5%% usage, got: %s", res.Output)
	}
}

func TestContextShortcut_Execute_FallsBackToTokenizerWhenProviderSilent(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetSessionTokensReturns(domain.SessionTokenStats{
		LastInputTokens: 0,
	})
	repo.GetMessageCountReturns(2)
	repo.GetMessagesReturns([]domain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User}},
		{Message: sdk.Message{Role: sdk.Assistant}},
	})

	model := &mockModelService{currentModel: "deepseek/deepseek-v4-flash"}
	tokenizer := &stubTokenEstimator{estimate: 6643}

	sc := NewContextShortcut(repo, model, tokenizer)
	res, err := sc.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(res.Output, "**Current Context Size:** ~6643 tokens (estimated)") {
		t.Errorf("expected estimated marker, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "**Usage:** 0.7%") {
		t.Errorf("expected 0.7%% usage from estimator, got: %s", res.Output)
	}
}

func TestContextShortcut_Execute_NoUsageNoEstimateOmitsPercent(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetSessionTokensReturns(domain.SessionTokenStats{LastInputTokens: 0})
	repo.GetMessageCountReturns(0)
	repo.GetMessagesReturns(nil)

	model := &mockModelService{currentModel: "deepseek/deepseek-v4-flash"}
	tokenizer := &stubTokenEstimator{estimate: 0}

	sc := NewContextShortcut(repo, model, tokenizer)
	res, err := sc.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if strings.Contains(res.Output, "**Usage:**") {
		t.Errorf("expected no usage line when both provider and estimator are empty, got: %s", res.Output)
	}
}
