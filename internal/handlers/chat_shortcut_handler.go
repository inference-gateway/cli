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
	stateManager *services.StateManager,
) tea.Cmd {
	return func() tea.Msg {
		if registryResult := s.tryExecuteFromRegistry(shortcut, args, stateManager); registryResult != nil {
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
func (s *ChatShortcutHandler) tryExecuteFromRegistry(shortcut string, args []string, stateManager *services.StateManager) tea.Msg {
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

	return s.executeRegistryShortcut(shortcutInstance, args, stateManager)
}

// executeRegistryShortcut executes a shortcut from the registry and handles results
func (s *ChatShortcutHandler) executeRegistryShortcut(shortcut shortcuts.Shortcut, args []string, stateManager *services.StateManager) tea.Msg {
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

	return s.handleShortcutSideEffect(result.SideEffect, result.Data, stateManager)
}

// handleShortcutSideEffect handles side effects from shortcut execution
func (s *ChatShortcutHandler) handleShortcutSideEffect(sideEffect shortcuts.SideEffectType, data any, stateManager *services.StateManager) tea.Msg {
	switch sideEffect {
	case shortcuts.SideEffectSwitchModel:
		return s.handleSwitchModelSideEffect(stateManager)
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
		return s.handleGenerateCommitSideEffect(data, stateManager)
	case shortcuts.SideEffectSaveConversation:
		return s.handleSaveConversationSideEffect()
	case shortcuts.SideEffectShowConversationSelection:
		return s.handleShowConversationSelectionSideEffect(stateManager)
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
func (s *ChatShortcutHandler) handleSwitchModelSideEffect(stateManager *services.StateManager) tea.Msg {
	_ = stateManager.TransitionToView(domain.ViewStateModelSelection)
	return domain.SetStatusEvent{
		Message:    "Select a model from the dropdown",
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
	return func() tea.Msg {
		ctx := context.Background()

		shortcut, exists := s.handler.shortcutRegistry.Get("compact")
		if !exists {
			return domain.SetStatusEvent{
				Message:    "Export command not found",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		exportShortcut, ok := shortcut.(*shortcuts.ExportShortcut)
		if !ok {
			return domain.SetStatusEvent{
				Message:    "Invalid export command type",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		filePath, err := exportShortcut.PerformExport(ctx)
		if err != nil {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Export failed: %v", err),
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("📝 Conversation exported to: %s", filePath),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
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

func (s *ChatShortcutHandler) handleGenerateCommitSideEffect(data any, stateManager *services.StateManager) tea.Msg {
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
		s.performCommitGeneration(data, stateManager),
	)()
}

func (s *ChatShortcutHandler) performCommitGeneration(data any, _ *services.StateManager) tea.Cmd {
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

func (s *ChatShortcutHandler) handleShowConversationSelectionSideEffect(stateManager *services.StateManager) tea.Msg {
	logger.Debug("handleShowConversationSelectionSideEffect called")

	if err := stateManager.TransitionToView(domain.ViewStateConversationSelection); err != nil {
		logger.Error("Failed to transition to conversation selection view", "error", err)
		return domain.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to show conversation selection: %v", err),
			Sticky: false,
		}
	}

	logger.Debug("Successfully transitioned to conversation selection view")

	return domain.SetStatusEvent{
		Message:    "Select a conversation from the dropdown",
		Spinner:    false,
		TokenUsage: s.handler.getCurrentTokenUsage(),
		StatusType: domain.StatusDefault,
	}
}
