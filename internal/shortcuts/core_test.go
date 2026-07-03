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
		LastInputTokens:   7_250,
		TotalInputTokens:  20_000,
		TotalOutputTokens: 314,
		TotalTokens:       20_314,
		RequestCount:      3,
	})
	repo.GetMessageCountReturns(8)

	model := &mockModelService{currentModel: "deepseek/deepseek-v4-flash"}
	tokenizer := &stubTokenEstimator{estimate: 999_999}

	sc := NewContextShortcut(repo, model, tokenizer)
	res, err := sc.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(res.Output, "**Current Context:** 7250 tokens") {
		t.Errorf("expected current context from LastInputTokens, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "**Session Totals:** 20000 input, 314 output") {
		t.Errorf("expected cumulative session totals line, got: %s", res.Output)
	}
	if strings.Contains(res.Output, "(estimated)") {
		t.Errorf("did not expect (estimated) marker when provider returned usage, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "**Usage:** 0.7%") {
		t.Errorf("expected 0.7%% usage (7250 last input / 1M window), got: %s", res.Output)
	}
}

func TestContextShortcut_Execute_FallsBackToTokenizerWhenProviderSilent(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetSessionTokensReturns(domain.SessionTokenStats{
		TotalInputTokens: 0,
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

	if !strings.Contains(res.Output, "**Current Context:** ~6643 tokens (estimated)") {
		t.Errorf("expected estimated marker, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "**Usage:** 0.7%") {
		t.Errorf("expected 0.7%% usage from estimator, got: %s", res.Output)
	}
}

func TestContextShortcut_Execute_NoUsageNoEstimateOmitsPercent(t *testing.T) {
	repo := &domainmocks.FakeConversationRepository{}
	repo.GetSessionTokensReturns(domain.SessionTokenStats{TotalInputTokens: 0})
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

func TestHelpShortcut_Execute_OpensOverlay(t *testing.T) {
	registry := NewRegistry()
	registry.Register(NewExitShortcut())
	help := NewHelpShortcut(registry)

	res, err := help.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if res.Output != "" {
		t.Errorf("expected empty output so nothing is appended to the conversation, got: %q", res.Output)
	}
	if res.SideEffect != SideEffectShowHelp {
		t.Errorf("expected SideEffectShowHelp to drive the overlay, got: %v", res.SideEffect)
	}
	if !res.Success {
		t.Error("expected Success to be true")
	}
}

func TestHelpShortcut_Execute_DetailForKnownShortcut(t *testing.T) {
	registry := NewRegistry()
	registry.Register(NewExitShortcut())
	help := NewHelpShortcut(registry)

	res, err := help.Execute(context.Background(), []string{"exit"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if !strings.Contains(res.Output, "exit") {
		t.Errorf("expected detail output to describe the 'exit' shortcut, got: %q", res.Output)
	}
	if res.SideEffect != SideEffectNone {
		t.Errorf("expected no side effect for a detail lookup, got: %v", res.SideEffect)
	}
}

func TestHelpShortcut_Execute_UnknownShortcut(t *testing.T) {
	registry := NewRegistry()
	help := NewHelpShortcut(registry)

	res, err := help.Execute(context.Background(), []string{"does-not-exist"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if res.Success {
		t.Error("expected Success to be false for an unknown shortcut")
	}
	if !strings.Contains(res.Output, "Unknown shortcut") {
		t.Errorf("expected an 'Unknown shortcut' message, got: %q", res.Output)
	}
}

func TestToolsShortcut_Execute_OpensToolsList(t *testing.T) {
	tools := NewToolsShortcut()

	if !tools.CanExecute(nil) {
		t.Error("expected /tools to accept no arguments")
	}
	if tools.CanExecute([]string{"extra"}) {
		t.Error("expected /tools to reject arguments")
	}

	res, err := tools.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if res.Output != "" {
		t.Errorf("expected empty output so nothing is appended to the conversation, got: %q", res.Output)
	}
	if res.SideEffect != SideEffectShowToolsList {
		t.Errorf("expected SideEffectShowToolsList to drive the view, got: %v", res.SideEffect)
	}
	if !res.Success {
		t.Error("expected Success to be true")
	}
}
