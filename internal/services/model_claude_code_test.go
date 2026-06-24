package services

import (
	"context"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestClaudeCodeListModelsArePrefixed(t *testing.T) {
	s := NewClaudeCodeModelService()
	models, err := s.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected a non-empty model list")
	}
	for _, m := range models {
		if !strings.HasPrefix(m, "anthropic/claude-") {
			t.Errorf("model %q is not anthropic/-prefixed", m)
		}
	}
}

// TestClaudeCodeModelsArePriced enforces the curate-to-priced invariant: every
// model offered in Claude Code mode must have a pricing entry so session cost can
// be computed from token counts.
func TestClaudeCodeModelsArePriced(t *testing.T) {
	s := NewClaudeCodeModelService()
	models, _ := s.ListModels(context.Background())
	for _, m := range models {
		if _, ok := config.DefaultModelPricing[m]; !ok {
			t.Errorf("model %q has no pricing entry (curate-to-priced invariant)", m)
		}
	}
}

func TestClaudeCodeModelAvailabilityAcceptsBareAndPrefixed(t *testing.T) {
	s := NewClaudeCodeModelService()

	if !s.IsModelAvailable("anthropic/claude-sonnet-4-5-20250929") {
		t.Error("prefixed id should be available")
	}
	if !s.IsModelAvailable("claude-sonnet-4-5-20250929") {
		t.Error("bare id should be available (back-compat)")
	}
	if s.IsModelAvailable("claude-3-haiku-20240307") {
		t.Error("dropped legacy model should not be available")
	}
	if err := s.ValidateModel("claude-opus-4-5-20251101"); err != nil {
		t.Errorf("bare valid id should validate: %v", err)
	}
}

func TestClaudeCodeSelectModelCanonicalizes(t *testing.T) {
	s := NewClaudeCodeModelService()
	if err := s.SelectModel("claude-opus-4-6"); err != nil {
		t.Fatalf("SelectModel: %v", err)
	}
	if got := s.GetCurrentModel(); got != "anthropic/claude-opus-4-6" {
		t.Errorf("GetCurrentModel = %q, want anthropic/claude-opus-4-6", got)
	}
}

func TestCanonicalClaudeModelID(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-6":           "anthropic/claude-opus-4-6",
		"anthropic/claude-opus-4-6": "anthropic/claude-opus-4-6",
		"":                          "",
		"deepseek/deepseek-v4":      "deepseek/deepseek-v4",
	}
	for in, want := range cases {
		if got := CanonicalClaudeModelID(in); got != want {
			t.Errorf("CanonicalClaudeModelID(%q) = %q, want %q", in, got, want)
		}
	}
}
