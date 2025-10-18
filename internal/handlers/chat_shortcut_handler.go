package handlers

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

// ChatShortcutHandler handles shortcut execution and side effects
type ChatShortcutHandler struct {
	handler *ChatHandler
}

// NewChatShortcutHandler creates a new shortcut handler
func NewChatShortcutHandler(handler *ChatHandler) *ChatShortcutHandler {
	return &ChatShortcutHandler{
		handler: handler,
	}
}

// executeShortcut executes the specific shortcut based on the shortcut type
func (s *ChatShortcutHandler) executeShortcut(
	shortcut string,
	args []string,
) tea.Cmd {
	return func() tea.Msg {
		if registryResult := s.tryExecuteFromRegistry(shortcut, args); registryResult != nil {
			return registryResult
		}

		switch shortcut {
		case "clear", "cls":
			if err := s.handler.conversationRepo.Clear(); err != nil {
				return domain.SetStatusEvent{
					Message:    fmt.Sprintf("Failed to clear conversation: %v", err),
					Spinner:    false,
					StatusType: domain.StatusDefault,
				}
			}

			if s.handler.messageQueue != nil {
				s.handler.messageQueue.Clear()
			}

			return tea.Batch(
				func() tea.Msg {
					return domain.UpdateHistoryEvent{
						History: s.handler.conversationRepo.GetMessages(),
					}
				},
				func() tea.Msg {
					return domain.SetStatusEvent{
						Message:    "Conversation cleared",
						Spinner:    false,
						StatusType: domain.StatusDefault,
					}
				},
			)()

		default:
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Unknown shortcut: %s", shortcut),
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}
	}
}

// tryExecuteFromRegistry attempts to execute shortcut from the shortcut registry
func (s *ChatShortcutHandler) tryExecuteFromRegistry(shortcut string, args []string) tea.Msg {
	if s.handler.shortcutRegistry == nil {
		return nil
	}

	shortcutInstance, exists := s.handler.shortcutRegistry.Get(shortcut)
	if !exists {
		return nil
	}

	if !shortcutInstance.CanExecute(args) {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Invalid usage. Usage: %s", shortcutInstance.GetUsage()),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	return s.executeRegistryShortcut(shortcutInstance, args)
}

// executeRegistryShortcut executes a shortcut from the registry and handles results
func (s *ChatShortcutHandler) executeRegistryShortcut(shortcut shortcuts.Shortcut, args []string) tea.Msg {
	ctx := context.Background()
	result, err := shortcut.Execute(ctx, args)
	if err != nil {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Command failed: %v", err),
			Spinner:    false,
			TokenUsage: s.handler.getCurrentTokenUsage(),
			StatusType: domain.StatusDefault,
		}
	}

	if result.Output != "" {
		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: result.Output,
			},
			Model: "",
			Time:  time.Now(),
		}

		if addErr := s.handler.conversationRepo.AddMessage(assistantEntry); addErr != nil {
			logger.Error("failed to add shortcut result message", "error", addErr)
		}

		if result.SideEffect == shortcuts.SideEffectNone {
			return tea.Batch(
				func() tea.Msg {
					return domain.UpdateHistoryEvent{
						History: s.handler.conversationRepo.GetMessages(),
					}
				},
				func() tea.Msg {
					return domain.SetStatusEvent{
						Message:    "Shortcut action completed",
						Spinner:    false,
						TokenUsage: s.handler.getCurrentTokenUsage(),
						StatusType: domain.StatusDefault,
					}
				},
			)()
		}
	}

	return s.handleShortcutSideEffect(result.SideEffect, result.Data)
}

// handleShortcutSideEffect handles side effects from shortcut execution
func (s *ChatShortcutHandler) handleShortcutSideEffect(sideEffect shortcuts.SideEffectType, data any) tea.Msg {
	switch sideEffect {
	case shortcuts.SideEffectSwitchModel:
		return s.handleSwitchModelSideEffect()
	case shortcuts.SideEffectSwitchTheme:
		return s.handleSwitchThemeSideEffect()
	case shortcuts.SideEffectClearConversation:
		return s.handleClearConversationSideEffect()
	case shortcuts.SideEffectExportConversation:
		return s.handleExportConversationSideEffect()
	case shortcuts.SideEffectReloadConfig:
		return s.handleReloadConfigSideEffect()
	case shortcuts.SideEffectShowHelp:
		return s.handleShowHelpSideEffect()
	case shortcuts.SideEffectExit:
		return tea.Quit()
	case shortcuts.SideEffectGenerateCommit:
		return s.handleGenerateCommitSideEffect(data)
	case shortcuts.SideEffectSaveConversation:
		return s.handleSaveConversationSideEffect()
	case shortcuts.SideEffectShowConversationSelection:
		return s.handleShowConversationSelectionSideEffect()
	case shortcuts.SideEffectStartNewConversation:
		return s.handleStartNewConversationSideEffect(data)
	case shortcuts.SideEffectShowA2AServers:
		return s.handleShowA2AServersSideEffect()
	case shortcuts.SideEffectShowA2ATaskManagement:
		return s.handleShowA2ATaskManagementSideEffect()
	default:
		return domain.SetStatusEvent{
			Message:    "Shortcut completed",
			Spinner:    false,
			TokenUsage: s.handler.getCurrentTokenUsage(),
			StatusType: domain.StatusDefault,
		}
	}
}

// Side effect handlers
func (s *ChatShortcutHandler) handleSwitchModelSideEffect() tea.Msg {
	_ = s.handler.stateManager.TransitionToView(domain.ViewStateModelSelection)
	return domain.SetStatusEvent{
		Message:    "Select a model from the dropdown",
		Spinner:    false,
		TokenUsage: s.handler.getCurrentTokenUsage(),
		StatusType: domain.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleSwitchThemeSideEffect() tea.Msg {
	_ = s.handler.stateManager.TransitionToView(domain.ViewStateThemeSelection)
	return domain.SetStatusEvent{
		Message:    "",
		Spinner:    false,
		TokenUsage: s.handler.getCurrentTokenUsage(),
		StatusType: domain.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleClearConversationSideEffect() tea.Msg {
	if err := s.handler.conversationRepo.Clear(); err != nil {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to clear conversation: %v", err),
			Spinner:    false,
			TokenUsage: s.handler.getCurrentTokenUsage(),
			StatusType: domain.StatusDefault,
		}
	}

	if s.handler.messageQueue != nil {
		s.handler.messageQueue.Clear()
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Conversation cleared",
				Spinner:    false,
				TokenUsage: s.handler.getCurrentTokenUsage(),
				StatusType: domain.StatusDefault,
			}
		},
	)()
}

func (s *ChatShortcutHandler) handleExportConversationSideEffect() tea.Msg {
	return tea.Batch(
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "📝 Generating summary and exporting conversation...",
				Spinner:    true,
				StatusType: domain.StatusWorking,
			}
		},
		s.performExportAsync(),
	)()
}

func (s *ChatShortcutHandler) performExportAsync() tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "🤖 Generating AI summary...",
				Spinner:    true,
				StatusType: domain.StatusGenerating,
			}
		},
		s.performSummaryGeneration(),
	)
}

func (s *ChatShortcutHandler) performSummaryGeneration() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		shortcut, exists := s.handler.shortcutRegistry.Get("compact")
		if !exists {
			return domain.SetStatusEvent{
				Message:    "Export command not found",
				Spinner:    false,
				StatusType: domain.StatusError,
			}
		}

		exportShortcut, ok := shortcut.(*shortcuts.ExportShortcut)
		if !ok {
			return domain.SetStatusEvent{
				Message:    "Invalid export command type",
				Spinner:    false,
				StatusType: domain.StatusError,
			}
		}

		exportResult, err := exportShortcut.PerformExport(ctx)
		if err != nil {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Export failed: %v", err),
				Spinner:    false,
				StatusType: domain.StatusError,
			}
		}

		if clearErr := s.handler.conversationRepo.ClearExceptFirstUserMessage(); clearErr != nil {
			logger.Error("failed to clear conversation except first message", "error", clearErr)
		}

		summaryEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: fmt.Sprintf("📝 **Conversation Summary**\n\n%s\n\n---\n\n*Full conversation exported to: %s*", exportResult.Summary, exportResult.FilePath),
			},
			Model: "",
			Time:  time.Now(),
		}

		if addErr := s.handler.conversationRepo.AddMessage(summaryEntry); addErr != nil {
			logger.Error("failed to add summary message", "error", addErr)
		}

		return tea.Batch(
			func() tea.Msg {
				return domain.UpdateHistoryEvent{
					History: s.handler.conversationRepo.GetMessages(),
				}
			},
			func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    fmt.Sprintf("📝 Conversation compacted and exported to: %s", exportResult.FilePath),
					Spinner:    false,
					StatusType: domain.StatusDefault,
				}
			},
		)()
	}
}

func (s *ChatShortcutHandler) handleReloadConfigSideEffect() tea.Msg {
	return domain.SetStatusEvent{
		Message:    "Configuration reloaded successfully",
		Spinner:    false,
		TokenUsage: s.handler.getCurrentTokenUsage(),
		StatusType: domain.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowHelpSideEffect() tea.Msg {
	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Help displayed",
				Spinner:    false,
				TokenUsage: s.handler.getCurrentTokenUsage(),
				StatusType: domain.StatusDefault,
			}
		},
	)()
}

func (s *ChatShortcutHandler) handleGenerateCommitSideEffect(data any) tea.Msg {
	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Generating AI commit message...",
				Spinner:    true,
				StatusType: domain.StatusWorking,
			}
		},
		s.performCommitGeneration(data),
	)()
}

func (s *ChatShortcutHandler) performCommitGeneration(data any) tea.Cmd {
	return func() tea.Msg {
		if data == nil {
			return domain.SetStatusEvent{
				Message:    "❌ No side effect data available",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			return domain.SetStatusEvent{
				Message:    "❌ Invalid side effect data format",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		ctx, ok1 := dataMap["context"].(context.Context)
		args, ok2 := dataMap["args"].([]string)
		diff, ok3 := dataMap["diff"].(string)

		if !ok1 || !ok2 || !ok3 {
			return domain.SetStatusEvent{
				Message:    "❌ Missing commit data",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		var result string
		var err error

		if gitShortcut, ok := dataMap["gitShortcut"].(*shortcuts.GitShortcut); ok {
			result, err = gitShortcut.PerformCommit(ctx, args, diff)
		} else {
			return domain.SetStatusEvent{
				Message:    "❌ Missing or invalid git shortcut data",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}
		if err != nil {
			errorEntry := domain.ConversationEntry{
				Message: sdk.Message{
					Role:    sdk.Assistant,
					Content: fmt.Sprintf("❌ **Commit Failed**\n\n%v", err),
				},
				Model: "",
				Time:  time.Now(),
			}

			if addErr := s.handler.conversationRepo.AddMessage(errorEntry); addErr != nil {
				logger.Error("failed to add commit error message", "error", addErr)
			}

			return tea.Batch(
				func() tea.Msg {
					return domain.UpdateHistoryEvent{
						History: s.handler.conversationRepo.GetMessages(),
					}
				},
				func() tea.Msg {
					return domain.SetStatusEvent{
						Message:    fmt.Sprintf("%s Commit failed: %v", icons.CrossMark, err),
						Spinner:    false,
						StatusType: domain.StatusDefault,
					}
				},
			)()
		}

		successEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: result,
			},
			Model: "",
			Time:  time.Now(),
		}

		if addErr := s.handler.conversationRepo.AddMessage(successEntry); addErr != nil {
			logger.Error("failed to add commit success message", "error", addErr)
		}

		return tea.Batch(
			func() tea.Msg {
				return domain.UpdateHistoryEvent{
					History: s.handler.conversationRepo.GetMessages(),
				}
			},
			func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    fmt.Sprintf("%s AI commit completed successfully", icons.CheckMark),
					Spinner:    false,
					StatusType: domain.StatusDefault,
				}
			},
		)()
	}
}

func (s *ChatShortcutHandler) handleSaveConversationSideEffect() tea.Msg {
	return domain.SetStatusEvent{
		Message:    "Conversation saved successfully",
		Spinner:    false,
		TokenUsage: s.handler.getCurrentTokenUsage(),
		StatusType: domain.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowConversationSelectionSideEffect() tea.Msg {
	if err := s.handler.stateManager.TransitionToView(domain.ViewStateConversationSelection); err != nil {
		logger.Error("Failed to transition to conversation selection view", "error", err)
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to show conversation selection: %v", err),
			Sticky: false,
		}
	}

	return domain.SetStatusEvent{
		Message:    "Select a conversation from the dropdown",
		Spinner:    false,
		TokenUsage: s.handler.getCurrentTokenUsage(),
		StatusType: domain.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowA2AServersSideEffect() tea.Msg {
	_ = s.handler.stateManager.TransitionToView(domain.ViewStateA2AServers)
	return domain.SetStatusEvent{
		Message:    "Loading A2A servers...",
		Spinner:    true,
		TokenUsage: s.handler.getCurrentTokenUsage(),
		StatusType: domain.StatusWorking,
	}
}

func (s *ChatShortcutHandler) handleStartNewConversationSideEffect(data any) tea.Msg {
	title, ok := data.(string)
	if !ok {
		title = "New Conversation"
	}

	// Check if we have a persistent conversation repository
	persistentRepo, ok := s.handler.conversationRepo.(*services.PersistentConversationRepository)
	if !ok {
		return domain.SetStatusEvent{
			Message:    "New conversation feature requires persistent storage to be enabled",
			Spinner:    false,
			TokenUsage: s.handler.getCurrentTokenUsage(),
			StatusType: domain.StatusDefault,
		}
	}

	// Start new conversation
	if err := persistentRepo.StartNewConversation(title); err != nil {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to start new conversation: %v", err),
			Spinner:    false,
			TokenUsage: s.handler.getCurrentTokenUsage(),
			StatusType: domain.StatusDefault,
		}
	}

	// Clear the current conversation history in the UI
	if err := s.handler.conversationRepo.Clear(); err != nil {
		logger.Error("failed to clear conversation UI after starting new", "error", err)
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("🆕 Started new conversation: %s", title),
				Spinner:    false,
				TokenUsage: s.handler.getCurrentTokenUsage(),
				StatusType: domain.StatusDefault,
			}
		},
	)()
}

func (s *ChatShortcutHandler) handleShowA2ATaskManagementSideEffect() tea.Msg {
	if err := s.handler.stateManager.TransitionToView(domain.ViewStateA2ATaskManagement); err != nil {
		logger.Error("Failed to transition to task management view", "error", err)
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to show task management: %v", err),
			Sticky: false,
		}
	}

	return domain.SetStatusEvent{
		Message:    "Task management interface",
		Spinner:    false,
		TokenUsage: s.handler.getCurrentTokenUsage(),
		StatusType: domain.StatusDefault,
	}
}
