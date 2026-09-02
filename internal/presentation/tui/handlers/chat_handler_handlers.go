package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

// FormatMetrics formats LLM completion metrics for the status bar, computing
// the request wall-clock time from the most recent user message.
func (h *ChatHandler) FormatMetrics(metrics *agentdomain.ChatMetrics) string {
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

	if metrics.Usage == nil {
		return strings.Join(parts, " | ")
	}

	if metrics.Usage.PromptTokens > 0 {
		parts = append(parts, fmt.Sprintf("Input: %d tokens", metrics.Usage.PromptTokens))
	}
	if details := metrics.Usage.PromptTokensDetails; details != nil && details.CachedTokens != nil && *details.CachedTokens > 0 {
		parts = append(parts, fmt.Sprintf("Cached: %d tokens", *details.CachedTokens))
	}
	if metrics.Usage.CompletionTokens > 0 {
		parts = append(parts, fmt.Sprintf("Output: %d tokens", metrics.Usage.CompletionTokens))
	}
	if metrics.Usage.TotalTokens > 0 {
		parts = append(parts, fmt.Sprintf("Total: %d tokens", metrics.Usage.TotalTokens))
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
	_ tui.FileSelectionRequestEvent,
) tea.Cmd {
	files, err := h.fileService.ListProjectFiles()
	if err != nil {
		return func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to load files: %v", err),
				Sticky: false,
			}
		}
	}

	if len(files) == 0 {
		return func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  "No files found in the current directory",
				Sticky: false,
			}
		}
	}

	if err := h.stateManager.TransitionToView(tui.ViewStateFileSelection); err != nil {
		return func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  "Failed to open file selection",
				Sticky: false,
			}
		}
	}

	return func() tea.Msg {
		return tui.SetupFileSelectionEvent{Files: files}
	}
}

// handleConversationSelected loads a persisted conversation from disk and
// refreshes the UI. Requires the conversation repo to be persistent; falls
// back with an error if it isn't.
func (h *ChatHandler) handleConversationSelected(
	msg tui.ConversationSelectedEvent,
) tea.Cmd {
	persistentRepo, ok := h.conversationRepo.(*conversation.PersistentConversationRepository)
	if !ok {
		return func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  "Conversation selection requires persistent storage",
				Sticky: false,
			}
		}
	}

	ctx := context.Background()
	if err := persistentRepo.LoadConversation(ctx, msg.ConversationID); err != nil {
		return func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to load conversation: %v", err),
				Sticky: false,
			}
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return tui.UpdateHistoryEvent{History: h.conversationRepo.GetMessages()}
		},
		func() tea.Msg {
			return tui.TodoUpdateEvent{Todos: nil}
		},
		func() tea.Msg {
			metadata := persistentRepo.GetCurrentConversationMetadata()
			return tui.SetStatusEvent{
				Message: fmt.Sprintf("Loaded conversation: %s (%d messages)",
					metadata.Title, metadata.MessageCount),
				Spinner:    false,
				StatusType: tui.StatusDefault,
			}
		},
	)
}

// handleMessageQueued refreshes history and emits a "Processing queued
// message..." status when the agent picks up a queued user message.
func (h *ChatHandler) handleMessageQueued() tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return tui.UpdateHistoryEvent{History: h.conversationRepo.GetMessages()}
		},
		func() tea.Msg {
			return tui.SetStatusEvent{
				Message:    "Processing queued message...",
				Spinner:    true,
				StatusType: tui.StatusProcessing,
			}
		},
	}

	return tea.Sequence(cmds...)
}

// HandleCommand parses a /shortcut and delegates to the registered handler.
func (h *ChatHandler) HandleCommand(commandText string) tea.Cmd {
	if h.shortcutRegistry == nil {
		return func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  "Shortcut registry not available",
				Sticky: false,
			}
		}
	}

	mainShortcut, args, err := h.shortcutRegistry.ParseShortcut(commandText)
	if err != nil {
		return func() tea.Msg {
			return tui.ShowErrorEvent{
				Error:  fmt.Sprintf("Invalid shortcut format: %v", err),
				Sticky: false,
			}
		}
	}

	return h.shortcutHandler.executeShortcut(mainShortcut, args)
}
