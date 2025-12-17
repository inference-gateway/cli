package handlers

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// MessageHistoryHandler handles message history navigation and restoration
type MessageHistoryHandler struct {
	stateManager     domain.StateManager
	conversationRepo domain.ConversationRepository
}

// NewMessageHistoryHandler creates a new message history handler
func NewMessageHistoryHandler(
	stateManager domain.StateManager,
	conversationRepo domain.ConversationRepository,
) *MessageHistoryHandler {
	return &MessageHistoryHandler{
		stateManager:     stateManager,
		conversationRepo: conversationRepo,
	}
}

// HandleNavigateBackInTime processes the navigate back in time event
func (h *MessageHistoryHandler) HandleNavigateBackInTime(event domain.NavigateBackInTimeEvent) tea.Cmd {
	return func() tea.Msg {
		entries := h.conversationRepo.GetMessages()

		userMessages := h.extractUserMessages(entries)

		if len(userMessages) == 0 {
			logger.Warn("No user messages to navigate back to")
			return nil
		}

		h.stateManager.SetupMessageHistoryState(userMessages)

		if err := h.stateManager.TransitionToView(domain.ViewStateMessageHistory); err != nil {
			logger.Error("Failed to transition to message history view", "error", err)
			return nil
		}

		return nil
	}
}

// HandleRestore processes the message history restore event
func (h *MessageHistoryHandler) HandleRestore(event domain.MessageHistoryRestoreEvent) tea.Cmd {
	return func() tea.Msg {
		if err := h.conversationRepo.DeleteMessagesAfterIndex(event.RestoreToIndex); err != nil {
			logger.Error("Failed to restore conversation", "error", err, "index", event.RestoreToIndex)
			return domain.ChatErrorEvent{
				RequestID: event.RequestID,
				Error:     err,
				Timestamp: time.Now(),
			}
		}

		h.stateManager.ClearMessageHistoryState()

		if err := h.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
			logger.Error("Failed to transition back to chat", "error", err)
		}

		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	}
}

// extractUserMessages filters conversation entries to only user messages
// and creates snapshots with truncated content for display
func (h *MessageHistoryHandler) extractUserMessages(entries []domain.ConversationEntry) []domain.UserMessageSnapshot {
	userMessages := make([]domain.UserMessageSnapshot, 0)

	for i, entry := range entries {
		if entry.Message.Role != sdk.User {
			continue
		}

		content, err := entry.Message.Content.AsMessageContent0()
		if err != nil {
			logger.Warn("Failed to extract message content", "error", err, "index", i)
			continue
		}

		if h.isSystemReminder(content) {
			continue
		}

		truncated := h.truncateMessage(content, 50)

		userMessage := domain.UserMessageSnapshot{
			Index:        i,
			Content:      content,
			Timestamp:    entry.Time,
			TruncatedMsg: truncated,
		}
		userMessages = append(userMessages, userMessage)
	}

	return userMessages
}

// isSystemReminder checks if a message content is a system reminder
func (h *MessageHistoryHandler) isSystemReminder(content string) bool {
	return strings.Contains(content, "<system-reminder>")
}

// truncateMessage truncates a message to the specified length
func (h *MessageHistoryHandler) truncateMessage(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen-3] + "..."
}
