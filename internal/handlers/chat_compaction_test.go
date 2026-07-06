package handlers

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestPlanExecutionContinuePrompt(t *testing.T) {
	t.Run("points the agent at the plan file when a path is given", func(t *testing.T) {
		got := planExecutionContinuePrompt(".infer/plans/2026-06-28-plan.md")
		assert.Contains(t, got, ".infer/plans/2026-06-28-plan.md")
		assert.Contains(t, got, "Read")
		assert.Contains(t, got, "fresh session")
	})

	t.Run("falls back to a generic prompt without a path", func(t *testing.T) {
		got := planExecutionContinuePrompt("")
		assert.NotContains(t, got, "Read that file")
		assert.Contains(t, got, "proceed")
	})
}

func TestNewSessionAfterPlanApproval(t *testing.T) {
	t.Run("starts a new empty session and adds a hidden continue message", func(t *testing.T) {
		repo := &mocks.FakeConversationRepository{}
		repo.GetCurrentConversationTitleReturns("Original")

		h := &ChatHandler{
			conversationRepo: repo,
		}

		h.newSessionAfterPlanApproval(".infer/plans/2026-06-28-plan.md")

		if repo.StartNewConversationCallCount() != 1 {
			t.Fatalf("expected StartNewConversation once, got %d", repo.StartNewConversationCallCount())
		}
		if repo.AddMessageCallCount() != 1 {
			t.Errorf("expected one hidden continue message, got %d adds", repo.AddMessageCallCount())
		}
	})

	t.Run("starts a new session even without a plan path", func(t *testing.T) {
		repo := &mocks.FakeConversationRepository{}
		repo.GetCurrentConversationTitleReturns("Original")

		h := &ChatHandler{
			conversationRepo: repo,
		}

		h.newSessionAfterPlanApproval("")

		if repo.StartNewConversationCallCount() != 1 {
			t.Fatalf("expected StartNewConversation once, got %d", repo.StartNewConversationCallCount())
		}
		if repo.AddMessageCallCount() != 1 {
			t.Errorf("expected one hidden continue message, got %d adds", repo.AddMessageCallCount())
		}
	})
}
