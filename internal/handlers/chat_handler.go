package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	spinner "github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	sdk "github.com/inference-gateway/sdk"
)

type ChatHandler struct {
	agentService          domain.AgentService
	conversationRepo      domain.ConversationRepository
	modelService          domain.ModelService
	configService         domain.ConfigService
	toolService           domain.ToolService
	fileService           domain.FileService
	imageService          domain.ImageService
	shortcutRegistry      *shortcuts.Registry
	stateManager          domain.StateManager
	messageQueue          domain.MessageQueue
	taskRetentionService  domain.TaskRetentionService
	backgroundTaskService domain.BackgroundTaskService
	agentManager          domain.AgentManager
	config                *config.Config

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
	imageService domain.ImageService,
	shortcutRegistry *shortcuts.Registry,
	stateManager domain.StateManager,
	messageQueue domain.MessageQueue,
	taskRetentionService domain.TaskRetentionService,
	backgroundTaskService domain.BackgroundTaskService,
	agentManager domain.AgentManager,
	cfg *config.Config,
) *ChatHandler {
	handler := &ChatHandler{
		agentService:          agentService,
		conversationRepo:      conversationRepo,
		modelService:          modelService,
		configService:         configService,
		toolService:           toolService,
		fileService:           fileService,
		imageService:          imageService,
		shortcutRegistry:      shortcutRegistry,
		stateManager:          stateManager,
		messageQueue:          messageQueue,
		agentManager:          agentManager,
		config:                cfg,
		taskRetentionService:  taskRetentionService,
		backgroundTaskService: backgroundTaskService,
	}

	handler.messageProcessor = NewChatMessageProcessor(handler)
	handler.commandHandler = NewChatCommandHandler(handler)
	handler.eventHandler = NewChatEventHandler(handler)

	return handler
}

// Handle routes incoming messages to appropriate handler methods based on message type.
// TODO - refactor this
func (h *ChatHandler) Handle(msg tea.Msg) tea.Cmd { // nolint:cyclop,gocyclo
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
	case domain.BashOutputChunkEvent:
		return h.HandleBashOutputChunkEvent(m)
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
	case domain.A2ATaskFailedEvent:
		return h.HandleA2ATaskFailedEvent(m)
	case domain.A2ATaskInputRequiredEvent:
		return h.HandleA2ATaskInputRequiredEvent(m)
	case domain.MessageQueuedEvent:
		return h.HandleMessageQueuedEvent(m)
	case domain.ToolApprovalRequestedEvent:
		return h.HandleToolApprovalRequestedEvent(m)
	case domain.ToolApprovalResponseEvent:
		return h.HandleToolApprovalResponseEvent(m)
	case domain.PlanApprovalRequestedEvent:
		return h.HandlePlanApprovalRequestedEvent(m)
	case domain.PlanApprovalResponseEvent:
		return h.HandlePlanApprovalResponseEvent(m)
	case domain.TodoUpdateChatEvent:
		return h.HandleTodoUpdateChatEvent(m)
	case domain.AgentStatusUpdateEvent:
		return h.HandleAgentStatusUpdateEvent(m)
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
			RequestID:  requestID,
			Model:      currentModel,
			Messages:   messages,
			IsChatMode: true,
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

		return domain.ChatStartEvent{
			RequestID: requestID,
			Model:     currentModel,
			Timestamp: time.Now(),
		}
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
			return domain.TodoUpdateEvent{
				Todos: nil,
			}
		},
		func() tea.Msg {
			metadata := persistentRepo.GetCurrentConversationMetadata()
			return domain.SetStatusEvent{
				Message: fmt.Sprintf("🔄 Loaded conversation: %s (%d messages)",
					metadata.Title, metadata.MessageCount),
				Spinner:    false,
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

func (h *ChatHandler) HandleToolApprovalRequestedEvent(
	msg domain.ToolApprovalRequestedEvent,
) tea.Cmd {
	return h.eventHandler.handleToolApprovalRequested(msg)
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

func (h *ChatHandler) HandleBashOutputChunkEvent(
	msg domain.BashOutputChunkEvent,
) tea.Cmd {
	return h.eventHandler.handleBashOutputChunk(msg)
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
	_, cmd := h.eventHandler.handleA2ATaskStatusUpdate(msg)
	return cmd
}

func (h *ChatHandler) HandleA2ATaskCompletedEvent(
	msg domain.A2ATaskCompletedEvent,
) tea.Cmd {
	_, cmd := h.eventHandler.handleA2ATaskCompleted(msg)
	return cmd
}

func (h *ChatHandler) HandleA2ATaskFailedEvent(
	msg domain.A2ATaskFailedEvent,
) tea.Cmd {
	_, cmd := h.eventHandler.handleA2ATaskFailed(msg)
	return cmd
}

func (h *ChatHandler) HandleA2ATaskInputRequiredEvent(
	msg domain.A2ATaskInputRequiredEvent,
) tea.Cmd {
	_, cmd := h.eventHandler.handleA2ATaskInputRequired(msg, h.stateManager)
	return cmd
}

func (h *ChatHandler) HandleMessageQueuedEvent(
	msg domain.MessageQueuedEvent,
) tea.Cmd {
	_, cmd := h.eventHandler.handleMessageQueued(msg)
	return cmd
}

func (h *ChatHandler) HandleToolApprovalResponseEvent(
	msg domain.ToolApprovalResponseEvent,
) tea.Cmd {
	return h.handleToolApprovalResponse(msg)
}

// HandleTodoUpdateChatEvent converts the chat event to a UI event for the todo component
func (h *ChatHandler) HandleTodoUpdateChatEvent(
	msg domain.TodoUpdateChatEvent,
) tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.TodoUpdateEvent{
			Todos: msg.Todos,
		}
	})

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil {
		cmds = append(cmds, h.listenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

// handleToolApprovalResponse processes the user's approval decision
func (h *ChatHandler) handleToolApprovalResponse(
	msg domain.ToolApprovalResponseEvent,
) tea.Cmd {
	if msg.Action == domain.ApprovalAutoAccept {
		h.stateManager.SetAgentMode(domain.AgentModeAutoAccept)

		approvalState := h.stateManager.GetApprovalUIState()
		if approvalState != nil && approvalState.ResponseChan != nil {
			select {
			case approvalState.ResponseChan <- domain.ApprovalApprove:
			default:
			}
		}

		h.stateManager.ClearApprovalUIState()
		_ = h.stateManager.TransitionToView(domain.ViewStateChat)

		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message: "Switched to Auto-Accept mode - all tools will be auto-approved",
				Spinner: false,
			}
		}
	}

	approvalState := h.stateManager.GetApprovalUIState()
	if approvalState != nil && approvalState.ResponseChan != nil {
		select {
		case approvalState.ResponseChan <- msg.Action:
		default:
		}
	}

	h.stateManager.ClearApprovalUIState()
	_ = h.stateManager.TransitionToView(domain.ViewStateChat)

	isManualExecution := strings.HasPrefix(msg.ToolCall.Id, "manual-")

	if !isManualExecution {
		return nil
	}

	if msg.Action == domain.ApprovalReject {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Tool execution rejected: %s", msg.ToolCall.Function.Name),
				Sticky: false,
			}
		}
	}

	return func() tea.Msg {
		ctx := context.WithValue(context.Background(), domain.ToolApprovedKey, true)
		result, err := h.toolService.ExecuteTool(ctx, msg.ToolCall.Function)
		if err != nil {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to execute tool: %v", err),
				Sticky: false,
			}
		}

		commandText := h.reconstructCommandText(msg.ToolCall)

		userEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent(commandText),
			},
			Time: time.Now(),
		}
		_ = h.conversationRepo.AddMessage(userEntry)

		responseContent := h.conversationRepo.FormatToolResultForLLM(result)
		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(responseContent),
			},
			Time: time.Now(),
		}
		_ = h.conversationRepo.AddMessage(assistantEntry)

		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	}
}

func (h *ChatHandler) HandlePlanApprovalRequestedEvent(
	msg domain.PlanApprovalRequestedEvent,
) tea.Cmd {
	if err := h.stateManager.TransitionToView(domain.ViewStatePlanApproval); err != nil {
		logger.Error("failed to transition to plan approval view", "error", err)
		return nil
	}

	h.stateManager.SetupPlanApprovalUIState(msg.PlanContent, msg.ResponseChan)

	return func() tea.Msg {
		return domain.ShowPlanApprovalEvent{
			PlanContent:  msg.PlanContent,
			ResponseChan: msg.ResponseChan,
		}
	}
}

func (h *ChatHandler) HandlePlanApprovalResponseEvent(
	msg domain.PlanApprovalResponseEvent,
) tea.Cmd {
	h.stateManager.ClearPlanApprovalUIState()

	if err := h.stateManager.TransitionToView(domain.ViewStateChat); err != nil {
		logger.Error("failed to transition back to chat view", "error", err)
		return nil
	}

	switch msg.Action {
	case domain.PlanApprovalAccept:
		// User accepted the plan - just clear and return to chat
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Plan accepted",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

	case domain.PlanApprovalAcceptAndAutoApprove:
		// User accepted and wants to enable auto-approve mode
		h.stateManager.SetAgentMode(domain.AgentModeAutoAccept)
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Plan accepted - Auto-Approve mode enabled",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}

	case domain.PlanApprovalReject:
		// User rejected the plan - keep in plan mode and notify
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Plan rejected - you can provide feedback or changes",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}
	}

	return nil
}

// reconstructCommandText reconstructs the original command text from a tool call
func (h *ChatHandler) reconstructCommandText(toolCall sdk.ChatCompletionMessageToolCall) string {
	if toolCall.Function.Name == "Bash" {
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
			if command, ok := args["command"].(string); ok {
				return "!" + command
			}
		}
		return "!<unknown command>"
	}

	return "!!" + toolCall.Function.Name + "(...)"
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
		domain.ThemeSelectedEvent,
		domain.ShowToolApprovalEvent,
		domain.ShowPlanApprovalEvent,
		tea.KeyMsg,
		tea.WindowSizeMsg,
		spinner.TickMsg:
		return true
	}

	return false
}

// HandleAgentStatusUpdateEvent handles agent status updates
func (h *ChatHandler) HandleAgentStatusUpdateEvent(msg domain.AgentStatusUpdateEvent) tea.Cmd {
	// The StateManager was already updated in the callback before this event was sent
	// Just receiving this event triggers a UI refresh, which updates the agent indicator

	// Check if all agents are ready - if so, stop polling
	if readiness := h.stateManager.GetAgentReadiness(); readiness != nil {
		if readiness.ReadyAgents >= readiness.TotalAgents {
			// All agents ready, stop polling
			return nil
		}
	}

	// Keep polling for status updates
	return func() tea.Msg {
		time.Sleep(500 * time.Millisecond)
		return domain.AgentStatusUpdateEvent{}
	}
}
