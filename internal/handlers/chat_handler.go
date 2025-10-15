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
	shared "github.com/inference-gateway/cli/internal/ui/shared"
	sdk "github.com/inference-gateway/sdk"
)

// ChatHandler handles chat-related messages using the new state management system
type ChatHandler struct {
	name             string
	agentService     domain.AgentService
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	configService    domain.ConfigService
	toolService      domain.ToolService
	fileService      domain.FileService
	shortcutRegistry *shortcuts.Registry

	// Embedded handlers for different concerns
	messageProcessor *ChatMessageProcessor
	commandHandler   *ChatCommandHandler
	eventHandler     *ChatEventHandler
	eventRegistry    *EventHandlerRegistry
}

// NewChatHandler creates a new chat handler
func NewChatHandler(
	agentService domain.AgentService,
	conversationRepo domain.ConversationRepository,
	modelService domain.ModelService,
	configService domain.ConfigService,
	toolService domain.ToolService,
	fileService domain.FileService,
	shortcutRegistry *shortcuts.Registry,
) *ChatHandler {
	handler := &ChatHandler{
		name:             "ChatHandler",
		agentService:     agentService,
		conversationRepo: conversationRepo,
		modelService:     modelService,
		configService:    configService,
		toolService:      toolService,
		fileService:      fileService,
		shortcutRegistry: shortcutRegistry,
	}

	handler.messageProcessor = NewChatMessageProcessor(handler)
	handler.commandHandler = NewChatCommandHandler(handler)
	handler.eventHandler = NewChatEventHandler(handler)
	handler.eventRegistry = NewEventHandlerRegistry()

	handler.registerEventHandlers()

	if err := handler.eventRegistry.ValidateAllEventTypes(); err != nil {
		panic(fmt.Sprintf("Event handler validation failed: %v", err))
	}

	return handler
}

// GetName returns the handler name
func (h *ChatHandler) GetName() string {
	return h.name
}

// GetPriority returns the handler priority
func (h *ChatHandler) GetPriority() int {
	return 100
}

// CanHandle determines if this handler can process the message
func (h *ChatHandler) CanHandle(msg tea.Msg) bool {
	switch msg.(type) {
	case domain.UserInputEvent:
		return true
	case domain.FileSelectionRequestEvent:
		return true
	case domain.ConversationSelectedEvent:
		return true
	case domain.ChatStartEvent, domain.ChatChunkEvent, domain.ChatCompleteEvent, domain.ChatErrorEvent:
		return true
	case domain.OptimizationStatusEvent:
		return true
	case domain.ToolCallPreviewEvent, domain.ToolCallUpdateEvent, domain.ToolCallReadyEvent:
		return true
	case domain.ToolExecutionStartedEvent, domain.ToolExecutionProgressEvent, domain.ToolExecutionCompletedEvent:
		return true
	case domain.ParallelToolsStartEvent, domain.ParallelToolsCompleteEvent:
		return true
	case domain.CancelledEvent:
		return true
	case domain.A2AToolCallExecutedEvent, domain.A2ATaskSubmittedEvent, domain.A2ATaskStatusUpdateEvent:
		return true
	case domain.A2ATaskCompletedEvent, domain.A2ATaskInputRequiredEvent:
		return true
	default:
		return false
	}
}

// Handle processes the message using the state manager
func (h *ChatHandler) Handle(
	msg tea.Msg,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case domain.UserInputEvent:
		return h.messageProcessor.handleUserInput(msg, stateManager)

	case domain.FileSelectionRequestEvent:
		return h.handleFileSelectionRequest(msg, stateManager)

	case domain.ConversationSelectedEvent:
		return h.handleConversationSelected(msg, stateManager)

	case domain.ChatStartEvent:
		return h.eventHandler.handleChatStart(msg, stateManager)

	case domain.ChatChunkEvent:
		return h.eventHandler.handleChatChunk(msg, stateManager)

	case domain.ToolCallPreviewEvent:
		return h.eventHandler.handleToolCallPreview(msg, stateManager)

	case domain.ToolCallUpdateEvent:
		return h.eventHandler.handleToolCallUpdate(msg, stateManager)

	case domain.ToolCallReadyEvent:
		return h.eventHandler.handleToolCallReady(msg, stateManager)

	case domain.ChatCompleteEvent:
		return h.eventHandler.handleChatComplete(msg, stateManager)

	case domain.ChatErrorEvent:
		return h.eventHandler.handleChatError(msg, stateManager)

	case domain.OptimizationStatusEvent:
		return h.eventHandler.handleOptimizationStatus(msg, stateManager)

	case domain.ToolExecutionStartedEvent:
		return h.eventHandler.handleToolExecutionStarted(msg, stateManager)

	case domain.ToolExecutionProgressEvent:
		return h.eventHandler.handleToolExecutionProgress(msg, stateManager)

	case domain.ToolExecutionCompletedEvent:
		return h.eventHandler.handleToolExecutionCompleted(msg, stateManager)

	case domain.ParallelToolsStartEvent:
		return h.eventHandler.handleParallelToolsStart(msg, stateManager)

	case domain.ParallelToolsCompleteEvent:
		return h.eventHandler.handleParallelToolsComplete(msg, stateManager)

	case domain.CancelledEvent:
		return h.eventHandler.handleCancelled(msg, stateManager)

	case domain.A2AToolCallExecutedEvent:
		return h.eventHandler.handleA2AToolCallExecuted(msg, stateManager)

	case domain.A2ATaskSubmittedEvent:
		return h.eventHandler.handleA2ATaskSubmitted(msg, stateManager)

	case domain.A2ATaskStatusUpdateEvent:
		return h.eventHandler.handleA2ATaskStatusUpdate(msg, stateManager)

	case domain.A2ATaskCompletedEvent:
		return h.eventHandler.handleA2ATaskCompleted(msg, stateManager)

	case domain.A2ATaskInputRequiredEvent:
		return h.eventHandler.handleA2ATaskInputRequired(msg, stateManager)

	default:
		logger.Warn("Unhandled event type received", "event_type", fmt.Sprintf("%T", msg))
		return h.handleUnknownEvent(msg, stateManager)
	}
}

// startChatCompletion initiates a chat completion request
func (h *ChatHandler) startChatCompletion(
	stateManager *services.StateManager,
) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		currentModel := h.modelService.GetCurrentModel()
		if currentModel == "" {
			return domain.ChatErrorEvent{
				RequestID: "unknown",
				Timestamp: time.Now(),
				Error:     fmt.Errorf("no model selected"),
			}
		}

		entries := h.conversationRepo.GetMessages()
		originalCount := len(entries)
		messages := make([]sdk.Message, originalCount)
		for i, entry := range entries {
			messages[i] = entry.Message
		}

		requestID := generateRequestID()

		req := &domain.AgentRequest{
			RequestID: requestID,
			Model:     currentModel,
			Messages:  messages,
		}

		eventChan, err := h.agentService.RunWithStream(ctx, req)
		if err != nil {
			return domain.ChatErrorEvent{
				RequestID: requestID,
				Timestamp: time.Now(),
				Error:     err,
			}
		}

		_ = stateManager.StartChatSession(requestID, currentModel, eventChan)

		return h.listenForChatEvents(eventChan)()
	}
}

// listenForChatEvents listens for chat events from the SDK
func (h *ChatHandler) listenForChatEvents(eventChan <-chan domain.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-eventChan; ok {
			return event
		}
		return nil
	}
}

// getCurrentTokenUsage returns current session token usage string
func (h *ChatHandler) getCurrentTokenUsage() string {
	messages := h.conversationRepo.GetMessages()
	if len(messages) == 0 {
		return ""
	}

	return shared.FormatCurrentTokenUsage(h.conversationRepo)
}

// FormatMetrics formats metrics for display (exposed for testing)
func (h *ChatHandler) FormatMetrics(metrics *domain.ChatMetrics) string {
	return h.eventHandler.FormatMetrics(metrics)
}

// ExtractMarkdownSummary extracts markdown summary (exposed for testing)
func (h *ChatHandler) ExtractMarkdownSummary(content string) (string, bool) {
	return h.messageProcessor.ExtractMarkdownSummary(content)
}

// ParseToolCall parses tool call syntax (exposed for testing)
func (h *ChatHandler) ParseToolCall(input string) (string, map[string]any, error) {
	return h.commandHandler.ParseToolCall(input)
}

// ParseArguments parses function arguments (exposed for testing)
func (h *ChatHandler) ParseArguments(argsStr string) (map[string]any, error) {
	return h.commandHandler.ParseArguments(argsStr)
}

func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

// handleUnknownEvent provides default pass-through behavior for unhandled events
func (h *ChatHandler) handleUnknownEvent(
	msg tea.Msg,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	chatSession := stateManager.GetChatSession()
	if chatSession != nil && chatSession.EventChannel != nil {
		return nil, h.listenForChatEvents(chatSession.EventChannel)
	}
	return nil, nil
}

// registerEventHandlers registers all event handlers with the registry
func (h *ChatHandler) registerEventHandlers() {
	h.eventRegistry.Register(domain.UserInputEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.messageProcessor.handleUserInput(msg.(domain.UserInputEvent), stateManager)
	})

	h.eventRegistry.Register(domain.FileSelectionRequestEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.handleFileSelectionRequest(msg.(domain.FileSelectionRequestEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ConversationSelectedEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.handleConversationSelected(msg.(domain.ConversationSelectedEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ChatStartEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleChatStart(msg.(domain.ChatStartEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ChatChunkEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleChatChunk(msg.(domain.ChatChunkEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ChatCompleteEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleChatComplete(msg.(domain.ChatCompleteEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ChatErrorEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleChatError(msg.(domain.ChatErrorEvent), stateManager)
	})

	h.eventRegistry.Register(domain.OptimizationStatusEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleOptimizationStatus(msg.(domain.OptimizationStatusEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ToolCallPreviewEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleToolCallPreview(msg.(domain.ToolCallPreviewEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ToolCallUpdateEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleToolCallUpdate(msg.(domain.ToolCallUpdateEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ToolCallReadyEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleToolCallReady(msg.(domain.ToolCallReadyEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ToolExecutionStartedEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleToolExecutionStarted(msg.(domain.ToolExecutionStartedEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ToolExecutionProgressEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleToolExecutionProgress(msg.(domain.ToolExecutionProgressEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ToolExecutionCompletedEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleToolExecutionCompleted(msg.(domain.ToolExecutionCompletedEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ParallelToolsStartEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleParallelToolsStart(msg.(domain.ParallelToolsStartEvent), stateManager)
	})

	h.eventRegistry.Register(domain.ParallelToolsCompleteEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleParallelToolsComplete(msg.(domain.ParallelToolsCompleteEvent), stateManager)
	})

	h.eventRegistry.Register(domain.CancelledEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleCancelled(msg.(domain.CancelledEvent), stateManager)
	})

	h.eventRegistry.Register(domain.A2AToolCallExecutedEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleA2AToolCallExecuted(msg.(domain.A2AToolCallExecutedEvent), stateManager)
	})

	h.eventRegistry.Register(domain.A2ATaskSubmittedEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleA2ATaskSubmitted(msg.(domain.A2ATaskSubmittedEvent), stateManager)
	})

	h.eventRegistry.Register(domain.A2ATaskStatusUpdateEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleA2ATaskStatusUpdate(msg.(domain.A2ATaskStatusUpdateEvent), stateManager)
	})

	h.eventRegistry.Register(domain.A2ATaskCompletedEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleA2ATaskCompleted(msg.(domain.A2ATaskCompletedEvent), stateManager)
	})

	h.eventRegistry.Register(domain.A2ATaskInputRequiredEvent{}, func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
		return h.eventHandler.handleA2ATaskInputRequired(msg.(domain.A2ATaskInputRequiredEvent), stateManager)
	})
}

// handleFileSelectionRequest handles the file selection request triggered by "@" key
func (h *ChatHandler) handleFileSelectionRequest(
	_ domain.FileSelectionRequestEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	files, err := h.fileService.ListProjectFiles()
	if err != nil {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to load files: %v", err),
				Sticky: false,
			}
		}
	}

	if len(files) == 0 {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No files found in the current directory",
				Sticky: false,
			}
		}
	}

	if err := stateManager.TransitionToView(domain.ViewStateFileSelection); err != nil {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Failed to open file selection",
				Sticky: false,
			}
		}
	}

	return nil, func() tea.Msg {
		return domain.SetupFileSelectionEvent{
			Files: files,
		}
	}
}

// handleConversationSelected handles conversation selection from dropdown
func (h *ChatHandler) handleConversationSelected(
	msg domain.ConversationSelectedEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	persistentRepo, ok := h.conversationRepo.(*services.PersistentConversationRepository)
	if !ok {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Conversation selection requires persistent storage",
				Sticky: false,
			}
		}
	}

	ctx := context.Background()
	if err := persistentRepo.LoadConversation(ctx, msg.ConversationID); err != nil {
		return nil, func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to load conversation: %v", err),
				Sticky: false,
			}
		}
	}

	return nil, tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			metadata := persistentRepo.GetCurrentConversationMetadata()
			return domain.SetStatusEvent{
				Message: fmt.Sprintf("🔄 Loaded conversation: %s (%d messages)",
					metadata.Title, metadata.MessageCount),
				Spinner:    false,
				TokenUsage: h.getCurrentTokenUsage(),
				StatusType: domain.StatusDefault,
			}
		},
	)
}
