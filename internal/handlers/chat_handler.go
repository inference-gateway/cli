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

type ChatHandler struct {
	agentService     domain.AgentService
	conversationRepo domain.ConversationRepository
	modelService     domain.ModelService
	configService    domain.ConfigService
	toolService      domain.ToolService
	fileService      domain.FileService
	shortcutRegistry *shortcuts.Registry
	stateManager     domain.StateManager

	messageProcessor *ChatMessageProcessor
	commandHandler   *ChatCommandHandler
	eventHandler     *ChatEventHandler
}

func NewChatHandler(
	agentService domain.AgentService,
	conversationRepo domain.ConversationRepository,
	modelService domain.ModelService,
	configService domain.ConfigService,
	toolService domain.ToolService,
	fileService domain.FileService,
	shortcutRegistry *shortcuts.Registry,
	stateManager domain.StateManager,
) *ChatHandler {
	handler := &ChatHandler{
		agentService:     agentService,
		conversationRepo: conversationRepo,
		modelService:     modelService,
		configService:    configService,
		toolService:      toolService,
		fileService:      fileService,
		shortcutRegistry: shortcutRegistry,
		stateManager:     stateManager,
	}

	handler.messageProcessor = NewChatMessageProcessor(handler)
	handler.commandHandler = NewChatCommandHandler(handler)
	handler.eventHandler = NewChatEventHandler(handler)

	return handler
}

// Handle routes incoming messages to appropriate handler methods based on message type.
// TODO - refactor this
func (h *ChatHandler) Handle(msg tea.Msg) tea.Cmd { // nolint:cyclop
	switch m := msg.(type) {
	case domain.UserInputEvent:
		return h.HandleUserInputEvent(m)
	case domain.FileSelectionRequestEvent:
		return h.HandleFileSelectionRequestEvent(m)
	case domain.ConversationSelectedEvent:
		return h.HandleConversationSelectedEvent(m)
	case domain.ChatStartEvent:
		return h.HandleChatStartEvent(m)
	case domain.ChatChunkEvent:
		return h.HandleChatChunkEvent(m)
	case domain.ChatCompleteEvent:
		return h.HandleChatCompleteEvent(m)
	case domain.ChatErrorEvent:
		return h.HandleChatErrorEvent(m)
	case domain.OptimizationStatusEvent:
		return h.HandleOptimizationStatusEvent(m)
	case domain.ToolCallPreviewEvent:
		return h.HandleToolCallPreviewEvent(m)
	case domain.ToolCallUpdateEvent:
		return h.HandleToolCallUpdateEvent(m)
	case domain.ToolCallReadyEvent:
		return h.HandleToolCallReadyEvent(m)
	case domain.ToolExecutionStartedEvent:
		return h.HandleToolExecutionStartedEvent(m)
	case domain.ToolExecutionProgressEvent:
		return h.HandleToolExecutionProgressEvent(m)
	case domain.ToolExecutionCompletedEvent:
		return h.HandleToolExecutionCompletedEvent(m)
	case domain.ParallelToolsStartEvent:
		return h.HandleParallelToolsStartEvent(m)
	case domain.ParallelToolsCompleteEvent:
		return h.HandleParallelToolsCompleteEvent(m)
	case domain.CancelledEvent:
		return h.HandleCancelledEvent(m)
	case domain.A2AToolCallExecutedEvent:
		return h.HandleA2AToolCallExecutedEvent(m)
	case domain.A2ATaskSubmittedEvent:
		return h.HandleA2ATaskSubmittedEvent(m)
	case domain.A2ATaskStatusUpdateEvent:
		return h.HandleA2ATaskStatusUpdateEvent(m)
	case domain.A2ATaskCompletedEvent:
		return h.HandleA2ATaskCompletedEvent(m)
	case domain.A2ATaskInputRequiredEvent:
		return h.HandleA2ATaskInputRequiredEvent(m)
	default:
		if isUIOnlyEvent(msg) {
			return nil
		}

		msgType := fmt.Sprintf("%T", msg)
		logger.Warn("unhandled domain event", "type", msgType)
		return nil
	}
}

func (h *ChatHandler) startChatCompletion() tea.Cmd {
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

		_ = h.stateManager.StartChatSession(requestID, currentModel, eventChan)

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
		return domain.SetupFileSelectionEvent{
			Files: files,
		}
	}
}

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
) tea.Cmd {
	return h.messageProcessor.handleUserInput(msg)
}

func (h *ChatHandler) HandleFileSelectionRequestEvent(
	msg domain.FileSelectionRequestEvent,
) tea.Cmd {
	return h.handleFileSelectionRequest(msg)
}

func (h *ChatHandler) HandleConversationSelectedEvent(
	msg domain.ConversationSelectedEvent,
) tea.Cmd {
	return h.handleConversationSelected(msg)
}

func (h *ChatHandler) HandleChatStartEvent(
	msg domain.ChatStartEvent,
) tea.Cmd {
	return h.eventHandler.handleChatStart(msg)
}

func (h *ChatHandler) HandleChatChunkEvent(
	msg domain.ChatChunkEvent,
) tea.Cmd {
	return h.eventHandler.handleChatChunk(msg)
}

func (h *ChatHandler) HandleChatCompleteEvent(
	msg domain.ChatCompleteEvent,
) tea.Cmd {
	return h.eventHandler.handleChatComplete(msg)
}

func (h *ChatHandler) HandleChatErrorEvent(
	msg domain.ChatErrorEvent,
) tea.Cmd {
	return h.eventHandler.handleChatError(msg)
}

func (h *ChatHandler) HandleOptimizationStatusEvent(
	msg domain.OptimizationStatusEvent,
) tea.Cmd {
	return h.eventHandler.handleOptimizationStatus(msg)
}

func (h *ChatHandler) HandleToolCallPreviewEvent(
	msg domain.ToolCallPreviewEvent,
) tea.Cmd {
	return h.eventHandler.handleToolCallPreview(msg)
}

func (h *ChatHandler) HandleToolCallUpdateEvent(
	msg domain.ToolCallUpdateEvent,
) tea.Cmd {
	return h.eventHandler.handleToolCallUpdate(msg)
}

func (h *ChatHandler) HandleToolCallReadyEvent(
	msg domain.ToolCallReadyEvent,
) tea.Cmd {
	return h.eventHandler.handleToolCallReady(msg)
}

func (h *ChatHandler) HandleToolExecutionStartedEvent(
	msg domain.ToolExecutionStartedEvent,
) tea.Cmd {
	return h.eventHandler.handleToolExecutionStarted(msg)
}

func (h *ChatHandler) HandleToolExecutionProgressEvent(
	msg domain.ToolExecutionProgressEvent,
) tea.Cmd {
	return h.eventHandler.handleToolExecutionProgress(msg)
}

func (h *ChatHandler) HandleToolExecutionCompletedEvent(
	msg domain.ToolExecutionCompletedEvent,
) tea.Cmd {
	return h.eventHandler.handleToolExecutionCompleted(msg)
}

func (h *ChatHandler) HandleParallelToolsStartEvent(
	msg domain.ParallelToolsStartEvent,
) tea.Cmd {
	return h.eventHandler.handleParallelToolsStart(msg)
}

func (h *ChatHandler) HandleParallelToolsCompleteEvent(
	msg domain.ParallelToolsCompleteEvent,
) tea.Cmd {
	return h.eventHandler.handleParallelToolsComplete(msg)
}

func (h *ChatHandler) HandleCancelledEvent(
	msg domain.CancelledEvent,
) tea.Cmd {
	return h.eventHandler.handleCancelled(msg)
}

func (h *ChatHandler) HandleA2AToolCallExecutedEvent(
	msg domain.A2AToolCallExecutedEvent,
) tea.Cmd {
	return h.eventHandler.handleA2AToolCallExecuted(msg)
}

func (h *ChatHandler) HandleA2ATaskSubmittedEvent(
	msg domain.A2ATaskSubmittedEvent,
) tea.Cmd {
	return h.eventHandler.handleA2ATaskSubmitted(msg)
}

func (h *ChatHandler) HandleA2ATaskStatusUpdateEvent(
	msg domain.A2ATaskStatusUpdateEvent,
) tea.Cmd {
	return h.eventHandler.handleA2ATaskStatusUpdate(msg)
}

func (h *ChatHandler) HandleA2ATaskCompletedEvent(
	msg domain.A2ATaskCompletedEvent,
) tea.Cmd {
	return h.eventHandler.handleA2ATaskCompleted(msg)
}

func (h *ChatHandler) HandleA2ATaskInputRequiredEvent(
	msg domain.A2ATaskInputRequiredEvent,
) tea.Cmd {
	return h.eventHandler.handleA2ATaskInputRequired(msg)
}

// isUIOnlyEvent checks if the event is a UI-only event that doesn't require business logic handling
func isUIOnlyEvent(msg tea.Msg) bool {
	switch msg.(type) {
	case domain.UpdateHistoryEvent,
		domain.StreamingContentEvent,
		domain.SetStatusEvent,
		domain.UpdateStatusEvent,
		domain.ShowErrorEvent,
		domain.ClearErrorEvent,
		domain.ClearInputEvent,
		domain.SetInputEvent,
		domain.ToggleHelpBarEvent,
		domain.HideHelpBarEvent,
		domain.DebugKeyEvent,
		domain.SetupFileSelectionEvent,
		domain.ScrollRequestEvent,
		domain.ConversationsLoadedEvent,
		domain.InitializeTextSelectionEvent,
		domain.ExitSelectionModeEvent,
		domain.ModelSelectedEvent,
		domain.ThemeSelectedEvent:
		return true
	}

	return false
}
