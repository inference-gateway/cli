package handlers

import (
	"fmt"
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
		messages := h.extractMessages(entries)

		if len(messages) == 0 {
			logger.Warn("No messages to navigate back to")
			return nil
		}

		return domain.MessageHistoryReadyEvent{
			Messages: messages,
		}
	}
}

// HandleRestore processes the message history restore event
func (h *MessageHistoryHandler) HandleRestore(event domain.MessageHistoryRestoreEvent) tea.Cmd {
	return func() tea.Msg {
		entries := h.conversationRepo.GetMessages()
		restoreIndex := h.adjustRestoreIndex(entries, event.RestoreToIndex)

		if err := h.conversationRepo.DeleteMessagesAfterIndex(restoreIndex); err != nil {
			logger.Error("Failed to restore conversation", "error", err, "index", restoreIndex)
			return domain.ChatErrorEvent{
				RequestID: event.RequestID,
				Error:     err,
				Timestamp: time.Now(),
			}
		}

		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	}
}

// HandleEdit processes the message history edit event
func (h *MessageHistoryHandler) HandleEdit(event domain.MessageHistoryEditEvent) tea.Cmd {
	return func() tea.Msg {
		entries := h.conversationRepo.GetMessages()
		if event.MessageIndex >= len(entries) {
			logger.Error("Invalid message index for edit", "index", event.MessageIndex)
			return domain.ChatErrorEvent{
				RequestID: event.RequestID,
				Error:     fmt.Errorf("invalid message index: %d", event.MessageIndex),
				Timestamp: time.Now(),
			}
		}

		msg := entries[event.MessageIndex]
		if msg.Message.Role != sdk.User {
			logger.Error("Cannot edit non-user message", "role", msg.Message.Role)
			return domain.ChatErrorEvent{
				RequestID: event.RequestID,
				Error:     fmt.Errorf("cannot edit %s message", msg.Message.Role),
				Timestamp: time.Now(),
			}
		}

		return domain.MessageHistoryEditReadyEvent{
			MessageIndex: event.MessageIndex,
			Content:      event.MessageContent,
			Snapshot:     event.MessageSnapshot,
		}
	}
}

// HandleEditSubmit processes the message edit submission
func (h *MessageHistoryHandler) HandleEditSubmit(event domain.MessageEditSubmitEvent) tea.Cmd {
	return func() tea.Msg {
		entries := h.conversationRepo.GetMessages()
		deleteIndex := h.adjustRestoreIndex(entries, event.OriginalIndex)

		if err := h.conversationRepo.DeleteMessagesAfterIndex(deleteIndex - 1); err != nil {
			logger.Error("Failed to delete messages during edit", "error", err)
			return domain.ChatErrorEvent{
				RequestID: event.RequestID,
				Error:     err,
				Timestamp: time.Now(),
			}
		}

		return domain.UserInputEvent{
			Content: event.EditedContent,
			Images:  event.Images,
		}
	}
}

// adjustRestoreIndex adjusts the restore index based on message role and tool calls
func (h *MessageHistoryHandler) adjustRestoreIndex(entries []domain.ConversationEntry, restoreIndex int) int {
	if restoreIndex >= len(entries) {
		return restoreIndex
	}

	msg := entries[restoreIndex]
	if msg.Message.Role == sdk.Assistant && msg.Message.ToolCalls != nil && len(*msg.Message.ToolCalls) > 0 {
		toolResponsesFound := 0
		for i := restoreIndex + 1; i < len(entries); i++ {
			if entries[i].Message.Role == sdk.Tool {
				restoreIndex = i
				toolResponsesFound++
			} else {
				break
			}
		}
		logger.Info("Adjusted restore point to after tool responses",
			"newIndex", restoreIndex,
			"toolResponsesFound", toolResponsesFound)
	} else {
		for restoreIndex > 0 && entries[restoreIndex].Message.Role == sdk.Tool {
			restoreIndex--
			logger.Info("Removed trailing tool message", "adjustedIndex", restoreIndex)
		}
	}

	return restoreIndex
}

// extractMessages filters conversation entries to user and assistant messages
// and creates snapshots with truncated content for display
func (h *MessageHistoryHandler) extractMessages(entries []domain.ConversationEntry) []domain.MessageSnapshot {
	messages := make([]domain.MessageSnapshot, 0)

	for i, entry := range entries {
		if entry.Message.Role != sdk.User && entry.Message.Role != sdk.Assistant {
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

		if strings.TrimSpace(content) == "" {
			continue
		}

		truncated := h.truncateMessage(content, 50)

		message := domain.MessageSnapshot{
			Index:        i,
			Role:         entry.Message.Role,
			Content:      content,
			Timestamp:    entry.Time,
			TruncatedMsg: truncated,
		}
		messages = append(messages, message)
	}

	return messages
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
