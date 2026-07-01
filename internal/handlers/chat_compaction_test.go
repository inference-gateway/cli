package handlers

import (
	"testing"

	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"

	domain "github.com/inference-gateway/cli/internal/domain"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// stubOptimizer is a minimal domain.ConversationOptimizer for tests: it returns
// a fixed message set regardless of input, letting a test dictate whether
// compaction "reduces" the conversation.
type stubOptimizer struct {
	out []sdk.Message
}

func (s stubOptimizer) OptimizeMessages(_ []sdk.Message, _ string, _ bool) []sdk.Message {
	return s.out
}

func entriesOfLen(n int) []domain.ConversationEntry {
	entries := make([]domain.ConversationEntry, n)
	for i := range entries {
		entries[i] = domain.ConversationEntry{
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("m")},
		}
	}
	return entries
}

func messagesOfLen(n int) []sdk.Message {
	msgs := make([]sdk.Message, n)
	for i := range msgs {
		msgs[i] = sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("s")}
	}
	return msgs
}

func TestPlanExecutionContinuePrompt(t *testing.T) {
	t.Run("points the agent at the plan file when a path is given", func(t *testing.T) {
		got := planExecutionContinuePrompt(".infer/plans/2026-06-28-plan.md")
		assert.Contains(t, got, ".infer/plans/2026-06-28-plan.md")
		assert.Contains(t, got, "Read")
	})

	t.Run("falls back to a generic prompt without a path", func(t *testing.T) {
		got := planExecutionContinuePrompt("")
		assert.NotContains(t, got, "Read that file")
		assert.Contains(t, got, "proceed")
	})
}

func TestCompactPlanningContext(t *testing.T) {
	t.Run("reseeds a new session when the optimizer reduces messages", func(t *testing.T) {
		repo := &mocks.FakeConversationRepository{}
		repo.GetMessagesReturns(entriesOfLen(3))
		repo.GetCurrentConversationTitleReturns("Original")
		model := &mocks.FakeModelService{}
		model.GetCurrentModelReturns("anthropic/claude")

		h := &ChatHandler{
			conversationOptimizer: stubOptimizer{out: messagesOfLen(1)},
			conversationRepo:      repo,
			modelService:          model,
		}

		if !h.compactPlanningContext() {
			t.Fatalf("expected compaction to reseed a new session")
		}
		if repo.StartNewConversationCallCount() != 1 {
			t.Fatalf("expected StartNewConversation once, got %d", repo.StartNewConversationCallCount())
		}
		if repo.AddMessageCallCount() != 1 {
			t.Errorf("expected the single optimized message to be re-added, got %d adds", repo.AddMessageCallCount())
		}
	})

	t.Run("does not reseed when the optimizer cannot reduce", func(t *testing.T) {
		repo := &mocks.FakeConversationRepository{}
		repo.GetMessagesReturns(entriesOfLen(3))
		model := &mocks.FakeModelService{}
		model.GetCurrentModelReturns("anthropic/claude")

		h := &ChatHandler{
			conversationOptimizer: stubOptimizer{out: messagesOfLen(3)},
			conversationRepo:      repo,
			modelService:          model,
		}

		if h.compactPlanningContext() {
			t.Fatalf("expected no reseed when nothing was compacted")
		}
		if repo.StartNewConversationCallCount() != 0 {
			t.Errorf("expected StartNewConversation not called, got %d", repo.StartNewConversationCallCount())
		}
	})

	t.Run("no-op when the optimizer is absent (defensive)", func(t *testing.T) {
		repo := &mocks.FakeConversationRepository{}
		repo.GetMessagesReturns(entriesOfLen(3))
		model := &mocks.FakeModelService{}
		model.GetCurrentModelReturns("anthropic/claude")

		h := &ChatHandler{
			conversationOptimizer: nil,
			conversationRepo:      repo,
			modelService:          model,
		}

		if h.compactPlanningContext() {
			t.Fatalf("expected no reseed when the optimizer is nil")
		}
		if repo.StartNewConversationCallCount() != 0 {
			t.Errorf("expected StartNewConversation not called")
		}
	})

	t.Run("no-op when no model is selected", func(t *testing.T) {
		repo := &mocks.FakeConversationRepository{}
		repo.GetMessagesReturns(entriesOfLen(3))
		model := &mocks.FakeModelService{}
		model.GetCurrentModelReturns("")

		h := &ChatHandler{
			conversationOptimizer: stubOptimizer{out: messagesOfLen(1)},
			conversationRepo:      repo,
			modelService:          model,
		}

		if h.compactPlanningContext() {
			t.Fatalf("expected no reseed when no model is selected")
		}
	})
}
