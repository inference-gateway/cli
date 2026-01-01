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

			s.handler.stateManager.SetTodos([]domain.TodoItem{})

			return tea.Batch(
				func() tea.Msg {
					return domain.UpdateHistoryEvent{
						History: s.handler.conversationRepo.GetMessages(),
					}
				},
				func() tea.Msg {
					return domain.TodoUpdateEvent{
						Todos: []domain.TodoItem{},
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

	sessionID := ""
	if persistentRepo, ok := s.handler.conversationRepo.(*services.PersistentConversationRepository); ok {
		sessionID = persistentRepo.GetCurrentConversationID()
		logger.Debug("Adding session ID to shortcut context", "session_id", sessionID, "shortcut", shortcut.GetName())
	} else {
		logger.Debug("ConversationRepo is not PersistentConversationRepository", "type", fmt.Sprintf("%T", s.handler.conversationRepo))
	}
	ctx = context.WithValue(ctx, domain.SessionIDKey, sessionID)

	result, err := shortcut.Execute(ctx, args)
	if err != nil {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Command failed: %v", err),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	if result.Output != "" {
		assistantEntry := domain.ConversationEntry{
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
					return domain.UpdateHistoryEvent{
						History: s.handler.conversationRepo.GetMessages(),
					}
				},
				func() tea.Msg {
					return domain.SetStatusEvent{
						Message:    "Shortcut action completed",
						Spinner:    false,
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
		return domain.SetStatusEvent{
			Message:    "Shortcut completed",
			Spinner:    false,
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
		StatusType: domain.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleSwitchThemeSideEffect() tea.Msg {
	_ = s.handler.stateManager.TransitionToView(domain.ViewStateThemeSelection)
	return domain.SetStatusEvent{
		Message:    "",
		Spinner:    false,
		StatusType: domain.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleClearConversationSideEffect() tea.Msg {
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
			return domain.TodoUpdateEvent{
				Todos: nil,
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
}

func (s *ChatShortcutHandler) handleReloadConfigSideEffect() tea.Msg {
	return domain.SetStatusEvent{
		Message:    "Configuration reloaded successfully",
		Spinner:    false,
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
				StatusType: domain.StatusDefault,
			}
		},
	)()
}

func (s *ChatShortcutHandler) handleSaveConversationSideEffect() tea.Msg {
	return domain.SetStatusEvent{
		Message:    "Conversation saved successfully",
		Spinner:    false,
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
		StatusType: domain.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleShowGithubActionSetupSideEffect() tea.Msg {
	return domain.TriggerGithubActionSetupEvent{}
}

func (s *ChatShortcutHandler) handleStartNewConversationSideEffect(data any) tea.Msg {
	title, ok := data.(string)
	if !ok {
		title = "New Conversation"
	}

	if err := s.handler.conversationRepo.StartNewConversation(title); err != nil {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to start new conversation: %v", err),
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
			return domain.TodoUpdateEvent{
				Todos: nil,
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("• Started new conversation: %s", title),
				Spinner:    false,
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

	hasBackgroundTasks := false
	if s.handler.backgroundTaskService != nil {
		backgroundTasks := s.handler.backgroundTaskService.GetBackgroundTasks()
		hasBackgroundTasks = len(backgroundTasks) > 0
	}

	return domain.SetStatusEvent{
		Message:    "Task management interface",
		Spinner:    hasBackgroundTasks,
		StatusType: domain.StatusDefault,
	}
}

func (s *ChatShortcutHandler) handleSetInputSideEffect(data any) tea.Msg {
	text, ok := data.(string)
	if !ok {
		return domain.SetStatusEvent{
			Message:    "Invalid input data",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	return domain.SetInputEvent{
		Text: text,
	}
}

func (s *ChatShortcutHandler) handleGenerateSnippetSideEffect(data any) tea.Msg {
	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Generating snippet with AI...",
				Spinner:    true,
				StatusType: domain.StatusWorking,
			}
		},
		s.performSnippetGeneration(data),
	)()
}

func (s *ChatShortcutHandler) performSnippetGeneration(data any) tea.Cmd {
	return func() tea.Msg {
		if data == nil {
			return domain.SetStatusEvent{
				Message:    "No snippet data available",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		dataMap, ok := data.(map[string]any)
		if !ok {
			return domain.SetStatusEvent{
				Message:    "Invalid snippet data format",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		ctx, ok1 := dataMap["context"].(context.Context)
		snippetDataMap, ok2 := dataMap["dataMap"].(map[string]string)
		customShortcut, ok3 := dataMap["customShortcut"].(*shortcuts.CustomShortcut)
		shortcutName, ok4 := dataMap["shortcutName"].(string)
		snippetConfig, ok5 := dataMap["snippet"].(*shortcuts.SnippetConfig)

		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
			return domain.SetStatusEvent{
				Message:    "Missing snippet generation data",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		snippet, err := customShortcut.GenerateSnippet(ctx, snippetDataMap, snippetConfig)
		if err != nil {
			return tea.Batch(
				func() tea.Msg {
					return domain.SetStatusEvent{
						Message:    fmt.Sprintf("%s Snippet generation failed: %v", icons.CrossMark, err),
						Spinner:    false,
						StatusType: domain.StatusDefault,
					}
				},
			)()
		}

		return tea.Batch(
			func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    fmt.Sprintf("%s Snippet generated for %s - review and press Enter", icons.CheckMark, shortcutName),
					Spinner:    false,
					StatusType: domain.StatusDefault,
				}
			},
			func() tea.Msg {
				return domain.SetInputEvent{
					Text: snippet,
				}
			},
		)()
	}
}

func (s *ChatShortcutHandler) handleCompactConversationSideEffect() tea.Msg {
	messageCount := s.handler.conversationRepo.GetMessageCount()
	if messageCount == 0 {
		return domain.SetStatusEvent{
			Message:    "No conversation to compact",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Compacting conversation history...",
				Spinner:    true,
				StatusType: domain.StatusWorking,
			}
		},
		s.performCompactAsync(),
	)()
}

func (s *ChatShortcutHandler) performCompactAsync() tea.Cmd {
	return func() tea.Msg {
		if s.handler.conversationOptimizer == nil {
			return domain.SetStatusEvent{
				Message:    "Conversation optimizer is not enabled in configuration",
				Spinner:    false,
				StatusType: domain.StatusError,
			}
		}

		entries := s.handler.conversationRepo.GetMessages()
		if len(entries) == 0 {
			return domain.SetStatusEvent{
				Message:    "No messages to compact",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		logger.Info("Starting conversation compaction", "message_count", len(entries))

		originalTitle := s.handler.conversationRepo.GetCurrentConversationTitle()

		messages := make([]sdk.Message, 0, len(entries))
		for _, entry := range entries {
			if entry.Hidden {
				continue
			}
			messages = append(messages, entry.Message)
		}

		currentModel := s.handler.modelService.GetCurrentModel()
		if currentModel == "" {
			return domain.SetStatusEvent{
				Message:    "No model selected - please select a model first",
				Spinner:    false,
				StatusType: domain.StatusError,
			}
		}

		logger.Info("Optimizing conversation", "model", currentModel, "message_count", len(messages))

		optimizedChan := make(chan []sdk.Message, 1)
		go func() {
			optimized := s.handler.conversationOptimizer.OptimizeMessages(messages, currentModel, true)
			optimizedChan <- optimized
		}()

		var optimizedMessages []sdk.Message
		select {
		case optimizedMessages = <-optimizedChan:
			logger.Info("Optimization complete", "original_count", len(messages), "optimized_count", len(optimizedMessages))
		case <-time.After(70 * time.Second):
			logger.Error("Optimization timed out after 70 seconds")
			return domain.SetStatusEvent{
				Message:    "Conversation optimization timed out - try again or check gateway logs",
				Spinner:    false,
				StatusType: domain.StatusError,
			}
		}

		if len(optimizedMessages) >= len(messages) {
			return domain.SetStatusEvent{
				Message:    "Conversation is already compact - no optimization needed",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		newTitle := fmt.Sprintf("Continued from %s", originalTitle)
		if err := s.handler.conversationRepo.StartNewConversation(newTitle); err != nil {
			logger.Error("Failed to start new conversation", "error", err)
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Failed to start new conversation: %v", err),
				Spinner:    false,
				StatusType: domain.StatusError,
			}
		}

		for _, msg := range optimizedMessages {
			entry := domain.ConversationEntry{
				Message: msg,
				Model:   currentModel,
				Time:    time.Now(),
			}
			if err := s.handler.conversationRepo.AddMessage(entry); err != nil {
				logger.Error("Failed to add optimized message", "error", err)
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
					Message:    fmt.Sprintf("• Started new conversation with summary (%d messages preserved)", len(messages)),
					Spinner:    false,
					StatusType: domain.StatusDefault,
				}
			},
		)()
	}
}

func (s *ChatShortcutHandler) handleEmbedImagesSideEffect(data any) tea.Msg {
	imageAttachments, ok := data.([]domain.ImageAttachment)
	if !ok {
		return domain.SetStatusEvent{
			Message:    "Invalid image data",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	var contentParts []sdk.ContentPart

	textPart, err := sdk.NewTextContentPart(fmt.Sprintf("The issue contains %d image(s):", len(imageAttachments)))
	if err != nil {
		logger.Warn("Failed to create text content part", "error", err)
	} else {
		contentParts = append(contentParts, textPart)
	}

	for i, img := range imageAttachments {
		dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)
		imagePart, err := sdk.NewImageContentPart(dataURL, nil)
		if err != nil {
			logger.Warn("Failed to create image content part", "index", i, "filename", img.Filename, "error", err)
			continue
		}
		contentParts = append(contentParts, imagePart)
	}

	if len(contentParts) == 0 {
		logger.Warn("No content parts created for image message")
		return domain.SetStatusEvent{
			Message:    "Failed to create image content",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	imageEntry := domain.ConversationEntry{
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
		return domain.SetStatusEvent{
			Message:    "Failed to embed images",
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
				Message:    fmt.Sprintf("Embedded %d image(s) from GitHub issue", len(imageAttachments)),
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	)()
}

// handleSendMessageWithModelSideEffect handles sending a message with a temporary model switch
func (s *ChatShortcutHandler) handleSendMessageWithModelSideEffect(data any) tea.Msg {
	if data == nil {
		return domain.SetStatusEvent{
			Message:    "No model switch data provided",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	switchData, ok := data.(shortcuts.ModelSwitchData)
	if !ok {
		logger.Error("Invalid model switch data type", "type", fmt.Sprintf("%T", data))
		return domain.SetStatusEvent{
			Message:    "Invalid model switch data",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	if err := s.handler.modelService.SelectModel(switchData.TargetModel); err != nil {
		logger.Error("Failed to switch to temporary model", "model", switchData.TargetModel, "error", err)
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to switch to model '%s': %v", switchData.TargetModel, err),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(switchData.Prompt),
		},
		Time: time.Now(),
	}

	if err := s.handler.conversationRepo.AddMessage(userEntry); err != nil {
		logger.Error("Failed to add message to conversation", "error", err)
		if restoreErr := s.handler.modelService.SelectModel(switchData.OriginalModel); restoreErr != nil {
			logger.Error("Failed to restore original model", "model", switchData.OriginalModel, "error", restoreErr)
		}
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to add message: %v", err),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	s.handler.pendingModelRestoration = switchData.OriginalModel

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Using model: %s", switchData.TargetModel),
				Spinner:    true,
				StatusType: domain.StatusPreparing,
			}
		},
		s.handler.startChatCompletion(),
	)()
}
