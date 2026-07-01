package handlers

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// compactionTimeout bounds the LLM summarization call so a wedged gateway can't
// hang the UI thread. Shared by /compact and the post-plan-approval flow.
const compactionTimeout = 70 * time.Second

// nonHiddenMessages returns the current conversation's user-visible messages
// (hidden system reminders / continue prompts are excluded from summarization).
func (h *ChatHandler) nonHiddenMessages() []sdk.Message {
	entries := h.conversationRepo.GetMessages()
	messages := make([]sdk.Message, 0, len(entries))
	for _, entry := range entries {
		if entry.Hidden {
			continue
		}
		messages = append(messages, entry.Message)
	}
	return messages
}

// optimizeWithTimeout force-compacts messages under a hard timeout. It returns
// (optimized, true) on success and (nil, false) if the summarization timed out.
func (h *ChatHandler) optimizeWithTimeout(messages []sdk.Message, model string) ([]sdk.Message, bool) {
	optimizedChan := make(chan []sdk.Message, 1)
	go func() {
		optimizedChan <- h.conversationOptimizer.OptimizeMessages(messages, model, true)
	}()

	select {
	case optimized := <-optimizedChan:
		return optimized, true
	case <-time.After(compactionTimeout):
		logger.Error("optimization timed out", "timeout", compactionTimeout)
		return nil, false
	}
}

// reseedConversationWithMessages saves the current conversation and starts a
// fresh one titled "Continued from <old title>", seeded with the given messages.
// The old conversation is preserved in storage; only the in-memory working set
// is replaced.
func (h *ChatHandler) reseedConversationWithMessages(messages []sdk.Message, model string) error {
	newTitle := fmt.Sprintf("Continued from %s", h.conversationRepo.GetCurrentConversationTitle())
	if err := h.conversationRepo.StartNewConversation(newTitle); err != nil {
		return err
	}
	for _, msg := range messages {
		entry := domain.ConversationEntry{
			Message: msg,
			Model:   model,
			Time:    time.Now(),
		}
		if err := h.conversationRepo.AddMessage(entry); err != nil {
			logger.Error("failed to add optimized message", "error", err)
		}
	}
	return nil
}

// addHiddenUserMessage appends a hidden user message to the current conversation.
func (h *ChatHandler) addHiddenUserMessage(content string) error {
	return h.conversationRepo.AddMessage(domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(content),
		},
		Time:   time.Now(),
		Hidden: true,
	})
}

// planExecutionContinuePrompt tells the agent to resume and execute an approved
// plan. When the planning context has been compacted away we point it back at
// the plan file on disk so it can recall the full plan without keeping it in
// context.
func planExecutionContinuePrompt(planPath string) string {
	if planPath == "" {
		return "The plan has been approved. Please proceed with executing it step by step. " +
			"Start by taking the first action required to implement the plan."
	}
	return fmt.Sprintf(
		"The plan has been approved and the planning context has been compacted to save space. "+
			"The full approved plan is saved at %s. Read that file to recall the plan, then execute "+
			"it step by step, starting with the first action required to implement it.",
		planPath,
	)
}

// compactPlanningContext force-compacts the (usually exploration-heavy) planning
// conversation into a fresh session. It returns true when a new session was
// seeded - meaning the prior messages, including the plan, were summarized away.
// Any failure returns false and leaves the conversation intact (fail open).
func (h *ChatHandler) compactPlanningContext() bool {
	messages := h.nonHiddenMessages()
	model := h.modelService.GetCurrentModel()
	if h.conversationOptimizer == nil || len(messages) == 0 || model == "" {
		return false
	}

	optimized, ok := h.optimizeWithTimeout(messages, model)
	if !ok || len(optimized) >= len(messages) {
		return false
	}

	if err := h.reseedConversationWithMessages(optimized, model); err != nil {
		logger.Error("failed to start new session after plan approval", "error", err)
		return false
	}

	logger.Info("compacted planning context after plan approval",
		"messages_before", len(messages), "messages_after", len(optimized))
	return true
}

// compactThenExecutePlanCmd compacts the planning conversation into a fresh
// session and then resumes the agent to execute the approved plan. Because the
// agent re-reads the plan from disk (planPath), the plan itself need not survive
// in context. When compaction reseeds a new session it also wiped the generic
// continue message the coordinator queued, so we re-add a plan-path-aware one;
// when it doesn't reseed (short conversation, timeout, disabled), that generic
// message and the plan are still in context, so we add nothing to avoid a dupe.
func (h *ChatHandler) compactThenExecutePlanCmd(planPath string) tea.Cmd {
	return func() tea.Msg {
		if h.compactPlanningContext() {
			if err := h.addHiddenUserMessage(planExecutionContinuePrompt(planPath)); err != nil {
				logger.Error("failed to add plan execution continue message", "error", err)
			}
		}

		return tea.Batch(
			func() tea.Msg {
				return domain.UpdateHistoryEvent{History: h.conversationRepo.GetMessages()}
			},
			h.startChatCompletion(),
		)()
	}
}
