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
	case domain.A2ATaskCompletedEvent:
		return true
	case domain.A2ATaskStatusUpdateEvent:
		return true
	case domain.MessageQueuedEvent:
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

	case domain.A2ATaskCompletedEvent:
		return h.eventHandler.handleA2ATaskCompleted(msg, stateManager)

	case domain.A2ATaskStatusUpdateEvent:
		return h.eventHandler.handleA2ATaskStatusUpdate(msg, stateManager)

	case domain.MessageQueuedEvent:
		return h.eventHandler.handleMessageQueued(msg, stateManager)

	}
	return nil, nil
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
			logger.Debug("Received chat event", "event_type", fmt.Sprintf("%T", event))
			return event
		}
		logger.Debug("Chat event channel closed")
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
