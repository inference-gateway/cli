package handlers

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
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
	case shortcuts.SideEffectExportConversation:
		return s.handleExportConversationSideEffect()
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
	case shortcuts.SideEffectShowA2AServers:
		return s.handleShowA2AServersSideEffect()
	case shortcuts.SideEffectShowInitGithubActionSetup:
		return s.handleShowGitHubAppSetupSideEffect()
	case shortcuts.SideEffectShowA2ATaskManagement:
		return s.handleShowA2ATaskManagementSideEffect()
	case shortcuts.SideEffectSetInput:
		return s.handleSetInputSideEffect(data)
	case shortcuts.SideEffectGenerateSnippet:
		return s.handleGenerateSnippetSideEffect(data)
	case shortcuts.SideEffectCompactConversation:
		return s.handleCompactConversationSideEffect()
	case shortcuts.SideEffectA2AAgentAdded:
		return s.handleA2AAgentAddedSideEffect(data)
	case shortcuts.SideEffectA2AAgentRemoved:
		if agentName, ok := data.(string); ok {
			return s.handleA2AAgentRemovedSideEffectWithData(agentName)
		}
		return s.handleA2AAgentRemovedSideEffect()
	case shortcuts.SideEffectEmbedImages:
		return s.handleEmbedImagesSideEffect(data)
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

		shortcut, exists := s.handler.shortcutRegistry.Get("export")
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
				Content: sdk.NewMessageContent(fmt.Sprintf("📝 **Conversation Summary**\n\n%s\n\n---\n\n*Full conversation exported to: %s*", exportResult.Summary, exportResult.FilePath)),
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
					Message:    fmt.Sprintf("Conversation exported to: %s", exportResult.FilePath),
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

func (s *ChatShortcutHandler) handleShowA2AServersSideEffect() tea.Msg {
	_ = s.handler.stateManager.TransitionToView(domain.ViewStateA2AServers)
	return domain.SetStatusEvent{
		Message:    "Loading A2A servers...",
		Spinner:    true,
		StatusType: domain.StatusWorking,
	}
}

func (s *ChatShortcutHandler) handleShowGitHubAppSetupSideEffect() tea.Msg {
	return domain.TriggerGitHubAppSetupEvent{}
}

func (s *ChatShortcutHandler) handleStartNewConversationSideEffect(data any) tea.Msg {
	title, ok := data.(string)
	if !ok {
		title = "New Conversation"
	}

	persistentRepo, ok := s.handler.conversationRepo.(*services.PersistentConversationRepository)
	if !ok {
		return domain.SetStatusEvent{
			Message:    "New conversation feature requires persistent storage to be enabled",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

	if err := persistentRepo.StartNewConversation(title); err != nil {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Failed to start new conversation: %v", err),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}

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
			return domain.TodoUpdateEvent{
				Todos: nil,
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("🆕 Started new conversation: %s", title),
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

		if !ok1 || !ok2 || !ok3 || !ok4 {
			return domain.SetStatusEvent{
				Message:    "Missing snippet generation data",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

		snippet, err := customShortcut.GenerateSnippet(ctx, snippetDataMap)
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

		messages := make([]sdk.Message, 0, len(entries))
		for _, entry := range entries {
			messages = append(messages, entry.Message)
		}

		currentModel := s.handler.modelService.GetCurrentModel()
		if currentModel == "" {
			logger.Warn("No current model set for compaction - will use basic summary")
		}
		logger.Info("About to optimize conversation", "model", currentModel, "message_count", len(messages))

		optimizedChan := make(chan []sdk.Message, 1)
		go func() {
			result := s.handler.conversationOptimizer.OptimizeMessagesWithModel(messages, currentModel, true)
			optimizedChan <- result
		}()

		var optimizedMessages []sdk.Message
		select {
		case optimizedMessages = <-optimizedChan:
			logger.Info("Optimization complete", "original_count", len(messages), "optimized_count", len(optimizedMessages))
		case <-time.After(70 * time.Second):
			logger.Error("Optimization timed out after 70 seconds")
			return domain.SetStatusEvent{
				Message:    "Conversation compaction timed out - try again or check gateway logs",
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

		if clearErr := s.handler.conversationRepo.Clear(); clearErr != nil {
			logger.Error("failed to clear conversation during compaction", "error", clearErr)
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Failed to compact conversation: %v", clearErr),
				Spinner:    false,
				StatusType: domain.StatusError,
			}
		}

		logger.Debug("Re-adding optimized messages", "count", len(optimizedMessages))
		for _, msg := range optimizedMessages {
			entry := domain.ConversationEntry{
				Message: msg,
				Time:    time.Now(),
			}
			if addErr := s.handler.conversationRepo.AddMessage(entry); addErr != nil {
				logger.Error("failed to add optimized message during compaction", "error", addErr)
			}
		}

		reduction := len(messages) - len(optimizedMessages)
		reductionPercent := (float64(reduction) / float64(len(messages))) * 100

		infoEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(fmt.Sprintf("Conversation compacted successfully! Reduced from %d to %d messages (%.1f%% reduction).", len(messages), len(optimizedMessages), reductionPercent)),
			},
			Model: "",
			Time:  time.Now(),
		}

		if addErr := s.handler.conversationRepo.AddMessage(infoEntry); addErr != nil {
			logger.Error("failed to add compact info message", "error", addErr)
		}

		logger.Debug("Compaction complete", "reduction", reduction, "reduction_percent", reductionPercent)

		return tea.Batch(
			func() tea.Msg {
				return domain.UpdateHistoryEvent{
					History: s.handler.conversationRepo.GetMessages(),
				}
			},
			func() tea.Msg {
				return domain.SetStatusEvent{
					Message:    fmt.Sprintf("Conversation compacted: %d messages reduced to %d", len(messages), len(optimizedMessages)),
					Spinner:    false,
					StatusType: domain.StatusDefault,
				}
			},
		)()
	}
}

func (s *ChatShortcutHandler) handleA2AAgentAddedSideEffect(data any) tea.Msg {
	if dataMap, ok := data.(map[string]any); ok {
		if shouldStart, ok := dataMap["start"].(bool); ok && shouldStart {
			if agent, ok := dataMap["agent"].(config.AgentEntry); ok {
				return s.handleA2AAgentAddedWithStart(agent)
			}
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
				Message:    "Agent added successfully",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	)()
}

func (s *ChatShortcutHandler) handleA2AAgentAddedWithStart(agent config.AgentEntry) tea.Msg {
	if s.handler.stateManager != nil {
		readiness := s.handler.stateManager.GetAgentReadiness()
		if readiness != nil {
			s.handler.stateManager.InitializeAgentReadiness(readiness.TotalAgents + 1)
		} else {
			s.handler.stateManager.InitializeAgentReadiness(1)
		}
	}

	go func() {
		ctx := context.Background()

		agentsConfig := &config.AgentsConfig{
			Agents: []config.AgentEntry{agent},
		}

		agentManager := services.NewAgentManager(s.handler.config, agentsConfig)
		agentManager.SetStatusCallback(func(agentName string, state domain.AgentState, message string, url string, image string) {
			if s.handler.stateManager != nil {
				s.handler.stateManager.UpdateAgentStatus(agentName, state, message, url, image)
			}
		})

		if err := agentManager.StartAgents(ctx); err != nil {
			logger.Error("Failed to start agent", "error", err, "agent", agent.Name)
			if s.handler.stateManager != nil {
				s.handler.stateManager.SetAgentError(agent.Name, err)
			}
		}
	}()

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Agent '%s' added and starting in background (check indicator below)", agent.Name),
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
		func() tea.Msg {
			time.Sleep(500 * time.Millisecond)
			return domain.AgentStatusUpdateEvent{}
		},
	)()
}

func (s *ChatShortcutHandler) handleA2AAgentRemovedSideEffect() tea.Msg {
	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Agent removed successfully",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	)()
}

func (s *ChatShortcutHandler) handleA2AAgentRemovedSideEffectWithData(agentName string) tea.Msg {
	if s.handler.stateManager != nil {
		s.handler.stateManager.RemoveAgent(agentName)
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: s.handler.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Agent removed successfully",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	)()
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
		logger.Debug("Added image to conversation", "index", i, "mime_type", img.MimeType, "filename", img.Filename)
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
