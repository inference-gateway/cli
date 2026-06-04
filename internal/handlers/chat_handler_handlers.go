package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
)

// FormatMetrics formats LLM completion metrics for the status bar, computing
// the request wall-clock time from the most recent user message.
func (h *ChatHandler) FormatMetrics(metrics *domain.ChatMetrics) string {
	if metrics == nil {
		return ""
	}

	var parts []string

	messages := h.conversationRepo.GetMessages()
	if len(messages) > 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Message.Role == sdk.User {
				actualDuration := time.Since(messages[i].Time).Round(time.Millisecond)
				parts = append(parts, fmt.Sprintf("Time: %v", actualDuration))
				break
			}
		}
	}

	if metrics.Usage != nil {
		if metrics.Usage.PromptTokens > 0 {
			parts = append(parts, fmt.Sprintf("Input: %d tokens", metrics.Usage.PromptTokens))
		}
		if metrics.Usage.CompletionTokens > 0 {
			parts = append(parts, fmt.Sprintf("Output: %d tokens", metrics.Usage.CompletionTokens))
		}
		if metrics.Usage.TotalTokens > 0 {
			parts = append(parts, fmt.Sprintf("Total: %d tokens", metrics.Usage.TotalTokens))
		}
	}

	return strings.Join(parts, " | ")
}

// ExtractMarkdownSummary delegates to ChatMessageProcessor.
func (h *ChatHandler) ExtractMarkdownSummary(content string) (string, bool) {
	return h.messageProcessor.ExtractMarkdownSummary(content)
}

// handleFileSelectionRequest lists project files and transitions the UI to
// the file-selection view. Stays on the orchestrator because it's a one-shot
// UI transition that doesn't fit any other service family.
func (h *ChatHandler) handleFileSelectionRequest(
	_ domain.FileSelectionRequestEvent,
) tea.Cmd {
	files, err := h.fileService.ListProjectFiles()
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to load files: %v", err),
				Sticky: false,
			}
		}
	}

	if len(files) == 0 {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No files found in the current directory",
				Sticky: false,
			}
		}
	}

	if err := h.stateManager.TransitionToView(domain.ViewStateFileSelection); err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Failed to open file selection",
				Sticky: false,
			}
		}
	}

	return func() tea.Msg {
		return domain.SetupFileSelectionEvent{Files: files}
	}
}

// handleConversationSelected loads a persisted conversation from disk and
// refreshes the UI. Requires the conversation repo to be persistent; falls
// back with an error if it isn't.
func (h *ChatHandler) handleConversationSelected(
	msg domain.ConversationSelectedEvent,
) tea.Cmd {
	persistentRepo, ok := h.conversationRepo.(*services.PersistentConversationRepository)
	if !ok {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Conversation selection requires persistent storage",
				Sticky: false,
			}
		}
	}

	ctx := context.Background()
	if err := persistentRepo.LoadConversation(ctx, msg.ConversationID); err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to load conversation: %v", err),
				Sticky: false,
			}
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{History: h.conversationRepo.GetMessages()}
		},
		func() tea.Msg {
			return domain.TodoUpdateEvent{Todos: nil}
		},
		func() tea.Msg {
			metadata := persistentRepo.GetCurrentConversationMetadata()
			return domain.SetStatusEvent{
				Message: fmt.Sprintf("Loaded conversation: %s (%d messages)",
					metadata.Title, metadata.MessageCount),
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	)
}

// handleMessageQueued refreshes history and emits a "Processing queued
// message..." status when the agent picks up a queued user message.
func (h *ChatHandler) handleMessageQueued() tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{History: h.conversationRepo.GetMessages()}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Processing queued message...",
				Spinner:    true,
				StatusType: domain.StatusProcessing,
			}
		},
	}

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

// HandleCommand parses a /shortcut and delegates to the registered handler.
func (h *ChatHandler) HandleCommand(commandText string) tea.Cmd {
	if h.shortcutRegistry == nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Shortcut registry not available",
				Sticky: false,
			}
		}
	}

	mainShortcut, args, err := h.shortcutRegistry.ParseShortcut(commandText)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Invalid shortcut format: %v", err),
				Sticky: false,
			}
		}
	}

	return h.shortcutHandler.executeShortcut(mainShortcut, args)
}
