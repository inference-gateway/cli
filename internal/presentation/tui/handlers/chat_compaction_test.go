package handlers

import (
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
)

func TestNewSessionAfterPlanApproval(t *testing.T) {
	t.Run("starts a new empty session and adds a hidden continue message", func(t *testing.T) {
		repo := &convmocks.FakeConversationRepository{}
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
		repo := &convmocks.FakeConversationRepository{}
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
