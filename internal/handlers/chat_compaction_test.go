package handlers

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestPlanExecutionContinuePrompt(t *testing.T) {
	t.Run("points the agent at the stored plan when an ID is given", func(t *testing.T) {
		got := planExecutionContinuePrompt("2026-06-28-090000-plan")
		assert.Contains(t, got, "infer plans show 2026-06-28-090000-plan")
		assert.Contains(t, got, "fresh session")
	})

	t.Run("falls back to a generic prompt without an ID", func(t *testing.T) {
		got := planExecutionContinuePrompt("")
		assert.NotContains(t, got, "infer plans show")
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

		h.newSessionAfterPlanApproval("2026-06-28-090000-plan")

		if repo.StartNewConversationCallCount() != 1 {
			t.Fatalf("expected StartNewConversation once, got %d", repo.StartNewConversationCallCount())
		}
		if repo.AddMessageCallCount() != 1 {
			t.Errorf("expected one hidden continue message, got %d adds", repo.AddMessageCallCount())
		}
	})

	t.Run("starts a new session even without a plan ID", func(t *testing.T) {
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
