package handlers

import (
	"context"
	"fmt"
	"os"
	"time"

	ui "github.com/inference-gateway/cli/internal/ui"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	gitdiff "github.com/inference-gateway/cli/internal/services/gitdiff"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
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
				return ui.SetStatusEvent{
					Message:    fmt.Sprintf("Failed to clear conversation: %v", err),
					Spinner:    false,
					StatusType: ui.StatusDefault,
				}
			}

			if s.handler.messageQueue != nil {
				s.handler.messageQueue.Clear()
			}

			s.handler.stateManager.SetTodos([]agentdomain.TodoItem{})

			return tea.Batch(
				func() tea.Msg {
					return ui.UpdateHistoryEvent{
						History: s.handler.conversationRepo.GetMessages(),
					}
				},
				func() tea.Msg {
					return ui.TodoUpdateEvent{
						Todos: []agentdomain.TodoItem{},
					}
				},
				func() tea.Msg {
					return ui.SetStatusEvent{
						Message:    "Conversation cleared",
						Spinner:    false,
						StatusType: ui.StatusDefault,
					}
				},
			)()

		default:
			return ui.SetStatusEvent{
				Message:    fmt.Sprintf("Unknown shortcut: %s", shortcut),
				Spinner:    false,
				StatusType: ui.StatusDefault,
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
		return ui.SetStatusEvent{
			Message:    fmt.Sprintf("Invalid usage. Usage: %s", shortcutInstance.GetUsage()),
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	return s.executeRegistryShortcut(shortcutInstance, args)
}

// executeRegistryShortcut executes a shortcut from the registry and handles results
func (s *ChatShortcutHandler) executeRegistryShortcut(shortcut shortcuts.Shortcut, args []string) tea.Msg {
	shortcutName := shortcut.GetName()
	if len(args) > 0 {
		shortcutName = fmt.Sprintf("%s %s", shortcutName, args[0])
	}

	return tea.Sequence(
		func() tea.Msg {
			return ui.SetStatusEvent{
				Message:    fmt.Sprintf("Executing %s...", shortcutName),
				Spinner:    true,
				StatusType: ui.StatusWorking,
			}
		},
		s.performShortcutExecution(shortcut, args),
	)()
}

// performShortcutExecution performs the async shortcut execution
func (s *ChatShortcutHandler) performShortcutExecution(shortcut shortcuts.Shortcut, args []string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		sessionID := ""
		if persistentRepo, ok := s.handler.conversationRepo.(*conversation.PersistentConversationRepository); ok {
			sessionID = persistentRepo.GetCurrentConversationID()
			logger.Debug("adding session ID to shortcut context", "session_id", sessionID, "shortcut", shortcut.GetName())
		} else {
			logger.Debug("conversationRepo is not PersistentConversationRepository", "type", fmt.Sprintf("%T", s.handler.conversationRepo))
		}
		ctx = context.WithValue(ctx, agentdomain.SessionIDKey, sessionID)

		result, err := shortcut.Execute(ctx, args)
		if err != nil {
			return ui.SetStatusEvent{
				Message:    fmt.Sprintf("Command failed: %v", err),
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		}

		if result.Output != "" {
			assistantEntry := convdomain.ConversationEntry{
				Message: sdk.Message{
					Role:    sdk.Assistant,
					Content: sdk.NewMessageContent(result.Output),
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
						return ui.UpdateHistoryEvent{
							History: s.handler.conversationRepo.GetMessages(),
						}
					},
					func() tea.Msg {
						return ui.SetStatusEvent{
							Message:    "Shortcut action completed",
							Spinner:    false,
							StatusType: ui.StatusDefault,
						}
					},
				)()
			}
		}

		return s.handleShortcutSideEffect(result.SideEffect, result.Data)
	}
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
	case shortcuts.SideEffectReloadConfig:
		return s.handleReloadConfigSideEffect()
	case shortcuts.SideEffectShowHelp:
		return s.handleShowHelpSideEffect()
	case shortcuts.SideEffectExit:
		return tea.Quit()
	case shortcuts.SideEffectSaveConversation:
		return s.handleSaveConversationSideEffect()
	case shortcuts.SideEffectShowConversationSelection:
		return s.handleShowConversationSelectionSideEffect()
	case shortcuts.SideEffectStartNewConversation:
		return s.handleStartNewConversationSideEffect(data)
	case shortcuts.SideEffectShowInitGithubActionSetup:
		return s.handleShowGithubActionSetupSideEffect()
	case shortcuts.SideEffectShowA2ATaskManagement:
		return s.handleShowA2ATaskManagementSideEffect()
	case shortcuts.SideEffectShowDiffViewer:
		return s.handleShowDiffViewerSideEffect()
	case shortcuts.SideEffectShowExplorer:
		return s.handleShowExplorerSideEffect()
	case shortcuts.SideEffectShowToolsList:
		return s.handleShowToolsListSideEffect()
	case shortcuts.SideEffectShowA2AAgents:
		return s.handleShowA2AAgentsSideEffect()
	case shortcuts.SideEffectSetInput:
		return s.handleSetInputSideEffect(data)
	case shortcuts.SideEffectGenerateSnippet:
		return s.handleGenerateSnippetSideEffect(data)
	case shortcuts.SideEffectCompactConversation:
		return s.handleCompactConversationSideEffect()
	case shortcuts.SideEffectEmbedImages:
		return s.handleEmbedImagesSideEffect(data)
	case shortcuts.SideEffectSendMessageWithModel:
		return s.handleSendMessageWithModelSideEffect(data)
	default:
		return ui.SetStatusEvent{
			Message:    "Shortcut completed",
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}
}

// Side effect handlers
func (s *ChatShortcutHandler) handleSwitchModelSideEffect() tea.Msg {
	_ = s.handler.stateManager.TransitionToView(ui.ViewStateModelSelection)
	return ui.SetStatusEvent{
		Message:    "Select a model from the dropdown",
		Spinner:    false,
		StatusType: ui.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleSwitchThemeSideEffect() tea.Msg {
	_ = s.handler.stateManager.TransitionToView(ui.ViewStateThemeSelection)
	return ui.SetStatusEvent{
		Message:    "",
		Spinner:    false,
		StatusType: ui.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowToolsListSideEffect() tea.Msg {
	_ = s.handler.stateManager.TransitionToView(ui.ViewStateToolsList)
	return ui.SetStatusEvent{
		Message:    "",
		Spinner:    false,
		StatusType: ui.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowA2AAgentsSideEffect() tea.Msg {
	_ = s.handler.stateManager.TransitionToView(ui.ViewStateA2AAgents)
	return ui.SetStatusEvent{
		Message:    "",
		Spinner:    false,
		StatusType: ui.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleClearConversationSideEffect() tea.Msg {
	if err := s.handler.conversationRepo.Clear(); err != nil {
		return ui.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to clear conversation: %v", err),
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	if s.handler.messageQueue != nil {
		s.handler.messageQueue.Clear()
	}

	return tea.Batch(
		func() tea.Msg {
			return ui.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return ui.TodoUpdateEvent{
				Todos: nil,
			}
		},
		func() tea.Msg {
			return ui.SetStatusEvent{
				Message:    "Conversation cleared",
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		},
	)()
}

func (s *ChatShortcutHandler) handleReloadConfigSideEffect() tea.Msg {
	return ui.SetStatusEvent{
		Message:    "Configuration reloaded successfully",
		Spinner:    false,
		StatusType: ui.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowHelpSideEffect() tea.Msg {
	return ui.TriggerHelpViewEvent{}
}

func (s *ChatShortcutHandler) handleSaveConversationSideEffect() tea.Msg {
	return ui.SetStatusEvent{
		Message:    "Conversation saved successfully",
		Spinner:    false,
		StatusType: ui.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowConversationSelectionSideEffect() tea.Msg {
	if err := s.handler.stateManager.TransitionToView(ui.ViewStateConversationSelection); err != nil {
		logger.Error("failed to transition to conversation selection view", "error", err)
		return ui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to show conversation selection: %v", err),
			Sticky: false,
		}
	}

	return ui.SetStatusEvent{
		Message:    "Select a conversation from the dropdown",
		Spinner:    false,
		StatusType: ui.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowGithubActionSetupSideEffect() tea.Msg {
	return ui.TriggerGithubActionSetupEvent{}
}

func (s *ChatShortcutHandler) handleStartNewConversationSideEffect(data any) tea.Msg {
	title, ok := data.(string)
	if !ok {
		title = "New Conversation"
	}

	if err := s.handler.conversationRepo.StartNewConversation(title); err != nil {
		return ui.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to start new conversation: %v", err),
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return ui.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return ui.TodoUpdateEvent{
				Todos: nil,
			}
		},
		func() tea.Msg {
			return ui.SetStatusEvent{
				Message:    fmt.Sprintf("• Started new conversation: %s", title),
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		},
	)()
}

func (s *ChatShortcutHandler) handleShowA2ATaskManagementSideEffect() tea.Msg {
	if err := s.handler.stateManager.TransitionToView(ui.ViewStateA2ATaskManagement); err != nil {
		logger.Error("failed to transition to task management view", "error", err)
		return ui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to show task management: %v", err),
			Sticky: false,
		}
	}

	hasBackgroundTasks := false
	if s.handler.backgroundTaskService != nil {
		backgroundTasks := s.handler.backgroundTaskService.GetBackgroundTasks()
		hasBackgroundTasks = len(backgroundTasks) > 0
	}

	return ui.SetStatusEvent{
		Message:    "Task management interface",
		Spinner:    hasBackgroundTasks,
		StatusType: ui.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowDiffViewerSideEffect() tea.Msg {
	cwd, err := os.Getwd()
	if err != nil {
		return ui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to resolve working directory: %v", err),
			Sticky: false,
		}
	}

	if !gitdiff.IsRepo(cwd) {
		return ui.SetStatusEvent{
			Message:    "Not a git repository - nothing to diff",
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	if err := s.handler.stateManager.TransitionToView(ui.ViewStateDiffViewer); err != nil {
		logger.Error("failed to transition to diff viewer view", "error", err)
		return ui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to open changes panel: %v", err),
			Sticky: false,
		}
	}

	return ui.SetStatusEvent{
		Message:    "Changes panel - ↑/↓ select · a stage · u unstage · c commit · esc back",
		Spinner:    false,
		StatusType: ui.StatusDefault,
	}
}

// handleShowExplorerSideEffect opens the file explorer panel. Unlike the diff
// viewer, it has no git-repository gate - it browses the working directory via
// the filesystem, so it works in any directory (git or not).
func (s *ChatShortcutHandler) handleShowExplorerSideEffect() tea.Msg {
	if err := s.handler.stateManager.TransitionToView(ui.ViewStateExplorer); err != nil {
		logger.Error("failed to transition to explorer view", "error", err)
		return ui.ShowErrorEvent{
			Error:  fmt.Sprintf("Failed to open explorer: %v", err),
			Sticky: false,
		}
	}

	return ui.SetStatusEvent{
		Message:    "Explorer - ↑/↓ select · →/← expand/collapse · / find · s select · v open · esc back",
		Spinner:    false,
		StatusType: ui.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleSetInputSideEffect(data any) tea.Msg {
	text, ok := data.(string)
	if !ok {
		return ui.SetStatusEvent{
			Message:    "Invalid input data",
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return ui.SetStatusEvent{
				Message:    "",
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		},
		func() tea.Msg {
			return ui.SetInputEvent{Text: text}
		},
	)()
}

func (s *ChatShortcutHandler) handleGenerateSnippetSideEffect(data any) tea.Msg {
	return tea.Batch(
		func() tea.Msg {
			return ui.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return ui.SetStatusEvent{
				Message:    "Generating snippet with AI...",
				Spinner:    true,
				StatusType: ui.StatusWorking,
			}
		},
		s.performSnippetGeneration(data),
	)()
}

func (s *ChatShortcutHandler) performSnippetGeneration(data any) tea.Cmd {
	return func() tea.Msg {
		if data == nil {
			return ui.SetStatusEvent{
				Message:    "No snippet data available",
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			return ui.SetStatusEvent{
				Message:    "Invalid snippet data format",
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		}

		ctx, ok1 := dataMap["context"].(context.Context)
		snippetDataMap, ok2 := dataMap["dataMap"].(map[string]string)
		customShortcut, ok3 := dataMap["customShortcut"].(*shortcuts.CustomShortcut)
		shortcutName, ok4 := dataMap["shortcutName"].(string)
		snippetConfig, ok5 := dataMap["snippet"].(*shortcuts.SnippetConfig)

		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
			return ui.SetStatusEvent{
				Message:    "Missing snippet generation data",
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		}

		snippet, err := customShortcut.GenerateSnippet(ctx, snippetDataMap, snippetConfig)
		if err != nil {
			return tea.Batch(
				func() tea.Msg {
					return ui.SetStatusEvent{
						Message:    fmt.Sprintf("%s Snippet generation failed: %v", icons.CrossMark, err),
						Spinner:    false,
						StatusType: ui.StatusDefault,
					}
				},
			)()
		}

		return tea.Batch(
			func() tea.Msg {
				return ui.SetStatusEvent{
					Message:    fmt.Sprintf("%s Snippet generated for %s - review and press Enter", icons.CheckMark, shortcutName),
					Spinner:    false,
					StatusType: ui.StatusDefault,
				}
			},
			func() tea.Msg {
				return ui.SetInputEvent{
					Text: snippet,
				}
			},
		)()
	}
}

func (s *ChatShortcutHandler) handleCompactConversationSideEffect() tea.Msg {
	messageCount := s.handler.conversationRepo.GetMessageCount()
	if messageCount == 0 {
		return ui.SetStatusEvent{
			Message:    "No conversation to compact",
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return ui.SetStatusEvent{
				Message:    "Compacting conversation history...",
				Spinner:    true,
				StatusType: ui.StatusWorking,
			}
		},
		s.performCompactAsync(),
	)()
}

func (s *ChatShortcutHandler) performCompactAsync() tea.Cmd {
	return func() tea.Msg {
		h := s.handler
		if h.conversationOptimizer == nil {
			return ui.SetStatusEvent{
				Message:    "Conversation optimizer is not enabled in configuration",
				Spinner:    false,
				StatusType: ui.StatusError,
			}
		}

		messages := h.nonHiddenMessages()
		if len(messages) == 0 {
			return ui.SetStatusEvent{
				Message:    "No messages to compact",
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		}

		currentModel := h.modelService.GetCurrentModel()
		if currentModel == "" {
			return ui.SetStatusEvent{
				Message:    "No model selected - please select a model first",
				Spinner:    false,
				StatusType: ui.StatusError,
			}
		}

		logger.Info("optimizing conversation", "model", currentModel, "message_count", len(messages))

		optimizedMessages, ok := h.optimizeWithTimeout(messages, currentModel)
		if !ok {
			return ui.SetStatusEvent{
				Message:    "Conversation optimization timed out - try again or check gateway logs",
				Spinner:    false,
				StatusType: ui.StatusError,
			}
		}
		logger.Info("optimization complete", "original_count", len(messages), "optimized_count", len(optimizedMessages))

		if len(optimizedMessages) >= len(messages) {
			return ui.SetStatusEvent{
				Message:    "Conversation is already compact - no optimization needed",
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		}

		if err := h.reseedConversationWithMessages(optimizedMessages, currentModel); err != nil {
			logger.Error("failed to start new conversation", "error", err)
			return ui.SetStatusEvent{
				Message:    fmt.Sprintf("Failed to start new conversation: %v", err),
				Spinner:    false,
				StatusType: ui.StatusError,
			}
		}

		return tea.Batch(
			func() tea.Msg {
				return ui.UpdateHistoryEvent{
					History: h.conversationRepo.GetMessages(),
				}
			},
			func() tea.Msg {
				return ui.SetStatusEvent{
					Message:    fmt.Sprintf("• Started new conversation with summary (%d messages preserved)", len(messages)),
					Spinner:    false,
					StatusType: ui.StatusDefault,
				}
			},
		)()
	}
}

func (s *ChatShortcutHandler) handleEmbedImagesSideEffect(data any) tea.Msg {
	imageAttachments, ok := data.([]agentdomain.ImageAttachment)
	if !ok {
		return ui.SetStatusEvent{
			Message:    "Invalid image data",
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	var contentParts []sdk.ContentPart

	textPart, err := sdk.NewTextContentPart(fmt.Sprintf("The issue contains %d image(s):", len(imageAttachments)))
	if err != nil {
		logger.Warn("failed to create text content part", "error", err)
	} else {
		contentParts = append(contentParts, textPart)
	}

	for i, img := range imageAttachments {
		dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)
		imagePart, err := sdk.NewImageContentPart(dataURL, nil)
		if err != nil {
			logger.Warn("failed to create image content part", "index", i, "filename", img.Filename, "error", err)
			continue
		}
		contentParts = append(contentParts, imagePart)
	}

	if len(contentParts) == 0 {
		logger.Warn("no content parts created for image message")
		return ui.SetStatusEvent{
			Message:    "Failed to create image content",
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	imageEntry := convdomain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(contentParts),
		},
		Images: imageAttachments,
		Time:   time.Now(),
		Hidden: true,
	}

	if err := s.handler.conversationRepo.AddMessage(imageEntry); err != nil {
		logger.Error("failed to add image message", "error", err)
		return ui.SetStatusEvent{
			Message:    "Failed to embed images",
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return ui.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return ui.SetStatusEvent{
				Message:    fmt.Sprintf("Embedded %d image(s) from GitHub issue", len(imageAttachments)),
				Spinner:    false,
				StatusType: ui.StatusDefault,
			}
		},
	)()
}

// handleSendMessageWithModelSideEffect handles sending a message with a temporary model switch
func (s *ChatShortcutHandler) handleSendMessageWithModelSideEffect(data any) tea.Msg {
	if data == nil {
		return ui.SetStatusEvent{
			Message:    "No model switch data provided",
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	switchData, ok := data.(shortcuts.ModelSwitchData)
	if !ok {
		logger.Error("invalid model switch data type", "type", fmt.Sprintf("%T", data))
		return ui.SetStatusEvent{
			Message:    "Invalid model switch data",
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	if err := s.handler.modelService.SelectModel(switchData.TargetModel); err != nil {
		logger.Error("failed to switch to temporary model", "model", switchData.TargetModel, "error", err)
		return ui.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to switch to model '%s': %v", switchData.TargetModel, err),
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	userEntry := convdomain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(switchData.Prompt),
		},
		Time: time.Now(),
	}

	if err := s.handler.conversationRepo.AddMessage(userEntry); err != nil {
		logger.Error("failed to add message to conversation", "error", err)
		if restoreErr := s.handler.modelService.SelectModel(switchData.OriginalModel); restoreErr != nil {
			logger.Error("failed to restore original model", "model", switchData.OriginalModel, "error", restoreErr)
		}
		return ui.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to add message: %v", err),
			Spinner:    false,
			StatusType: ui.StatusDefault,
		}
	}

	if s.handler.completionRunner != nil {
		s.handler.completionRunner.SetPendingRestoration(switchData.OriginalModel)
	}

	return tea.Batch(
		func() tea.Msg {
			return ui.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return ui.SetStatusEvent{
				Message:    fmt.Sprintf("Using model: %s", switchData.TargetModel),
				Spinner:    true,
				StatusType: ui.StatusPreparing,
			}
		},
		s.handler.startChatCompletion(),
	)()
}
