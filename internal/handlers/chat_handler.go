package handlers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	shared "github.com/inference-gateway/cli/internal/ui/shared"
	sdk "github.com/inference-gateway/sdk"
)

type ChatHandler struct {
	name             string
	agentService     domain.AgentService
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	configService    domain.ConfigService
	toolService      domain.ToolService
	fileService      domain.FileService
	shortcutRegistry *shortcuts.Registry

	messageProcessor *ChatMessageProcessor
	commandHandler   *ChatCommandHandler
	eventHandler     *ChatEventHandler
	eventRegistry    *EventRegistry
}

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
	handler.eventRegistry = NewEventRegistry(handler)

	return handler
}

func (h *ChatHandler) GetName() string {
	return h.name
}

func (h *ChatHandler) GetPriority() int {
	return 100
}

func (h *ChatHandler) CanHandle(msg tea.Msg) bool {
	_, exists := h.eventRegistry.handlers[reflect.TypeOf(msg)]
	return exists
}

func (h *ChatHandler) Handle(
	msg tea.Msg,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventRegistry.Handle(h, msg, stateManager)
}

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

func (h *ChatHandler) listenForChatEvents(eventChan <-chan domain.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-eventChan; ok {
			return event
		}
		return nil
	}
}

func (h *ChatHandler) getCurrentTokenUsage() string {
	messages := h.conversationRepo.GetMessages()
	if len(messages) == 0 {
		return ""
	}

	return shared.FormatCurrentTokenUsage(h.conversationRepo)
}

func (h *ChatHandler) FormatMetrics(metrics *domain.ChatMetrics) string {
	return h.eventHandler.FormatMetrics(metrics)
}

func (h *ChatHandler) ExtractMarkdownSummary(content string) (string, bool) {
	return h.messageProcessor.ExtractMarkdownSummary(content)
}

func (h *ChatHandler) ParseToolCall(input string) (string, map[string]any, error) {
	return h.commandHandler.ParseToolCall(input)
}

func (h *ChatHandler) ParseArguments(argsStr string) (map[string]any, error) {
	return h.commandHandler.ParseArguments(argsStr)
}

func generateRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

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

func (h *ChatHandler) HandleUserInputEvent(
	msg domain.UserInputEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.messageProcessor.handleUserInput(msg, stateManager)
}

func (h *ChatHandler) HandleFileSelectionRequestEvent(
	msg domain.FileSelectionRequestEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.handleFileSelectionRequest(msg, stateManager)
}

func (h *ChatHandler) HandleConversationSelectedEvent(
	msg domain.ConversationSelectedEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.handleConversationSelected(msg, stateManager)
}

func (h *ChatHandler) HandleChatStartEvent(
	msg domain.ChatStartEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleChatStart(msg, stateManager)
}

func (h *ChatHandler) HandleChatChunkEvent(
	msg domain.ChatChunkEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleChatChunk(msg, stateManager)
}

func (h *ChatHandler) HandleChatCompleteEvent(
	msg domain.ChatCompleteEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleChatComplete(msg, stateManager)
}

func (h *ChatHandler) HandleChatErrorEvent(
	msg domain.ChatErrorEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleChatError(msg, stateManager)
}

func (h *ChatHandler) HandleOptimizationStatusEvent(
	msg domain.OptimizationStatusEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleOptimizationStatus(msg, stateManager)
}

func (h *ChatHandler) HandleToolCallPreviewEvent(
	msg domain.ToolCallPreviewEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleToolCallPreview(msg, stateManager)
}

func (h *ChatHandler) HandleToolCallUpdateEvent(
	msg domain.ToolCallUpdateEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleToolCallUpdate(msg, stateManager)
}

func (h *ChatHandler) HandleToolCallReadyEvent(
	msg domain.ToolCallReadyEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleToolCallReady(msg, stateManager)
}

func (h *ChatHandler) HandleToolExecutionStartedEvent(
	msg domain.ToolExecutionStartedEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleToolExecutionStarted(msg, stateManager)
}

func (h *ChatHandler) HandleToolExecutionProgressEvent(
	msg domain.ToolExecutionProgressEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleToolExecutionProgress(msg, stateManager)
}

func (h *ChatHandler) HandleToolExecutionCompletedEvent(
	msg domain.ToolExecutionCompletedEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleToolExecutionCompleted(msg, stateManager)
}

func (h *ChatHandler) HandleParallelToolsStartEvent(
	msg domain.ParallelToolsStartEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleParallelToolsStart(msg, stateManager)
}

func (h *ChatHandler) HandleParallelToolsCompleteEvent(
	msg domain.ParallelToolsCompleteEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleParallelToolsComplete(msg, stateManager)
}

func (h *ChatHandler) HandleCancelledEvent(
	msg domain.CancelledEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleCancelled(msg, stateManager)
}

func (h *ChatHandler) HandleA2AToolCallExecutedEvent(
	msg domain.A2AToolCallExecutedEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleA2AToolCallExecuted(msg, stateManager)
}

func (h *ChatHandler) HandleA2ATaskSubmittedEvent(
	msg domain.A2ATaskSubmittedEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleA2ATaskSubmitted(msg, stateManager)
}

func (h *ChatHandler) HandleA2ATaskStatusUpdateEvent(
	msg domain.A2ATaskStatusUpdateEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleA2ATaskStatusUpdate(msg, stateManager)
}

func (h *ChatHandler) HandleA2ATaskCompletedEvent(
	msg domain.A2ATaskCompletedEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleA2ATaskCompleted(msg, stateManager)
}

func (h *ChatHandler) HandleA2ATaskInputRequiredEvent(
	msg domain.A2ATaskInputRequiredEvent,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	return h.eventHandler.handleA2ATaskInputRequired(msg, stateManager)
}
