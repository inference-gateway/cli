package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	services "github.com/inference-gateway/cli/internal/services"
	tools "github.com/inference-gateway/cli/internal/services/tools"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	utils "github.com/inference-gateway/cli/internal/utils"
	sdk "github.com/inference-gateway/sdk"
)

type ChatHandler struct {
	agentService            domain.AgentService
	conversationRepo        domain.ConversationRepository
	conversationOptimizer   domain.ConversationOptimizerService
	modelService            domain.ModelService
	configService           domain.ConfigService
	toolService             domain.ToolService
	fileService             domain.FileService
	imageService            domain.ImageService
	shortcutRegistry        *shortcuts.Registry
	stateManager            domain.StateManager
	messageQueue            domain.MessageQueue
	taskRetentionService    domain.TaskRetentionService
	backgroundTaskService   domain.BackgroundTaskService
	backgroundShellService  domain.BackgroundShellService
	agentManager            domain.AgentManager
	config                  *config.Config
	messageProcessor        *ChatMessageProcessor
	shortcutHandler         *ChatShortcutHandler
	bashDetachChan          chan<- struct{}
	bashDetachChanMu        sync.RWMutex
	bashEventChannel        <-chan tea.Msg
	bashEventChannelMu      sync.RWMutex
	toolEventChannel        <-chan tea.Msg
	toolEventChannelMu      sync.RWMutex
	activeToolCallID        string
	pendingModelRestoration string
}

func NewChatHandler(
	agentService domain.AgentService,
	conversationRepo domain.ConversationRepository,
	conversationOptimizer domain.ConversationOptimizerService,
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
	backgroundShellService domain.BackgroundShellService,
	agentManager domain.AgentManager,
	cfg *config.Config,
) *ChatHandler {
	handler := &ChatHandler{
		agentService:           agentService,
		conversationRepo:       conversationRepo,
		conversationOptimizer:  conversationOptimizer,
		modelService:           modelService,
		configService:          configService,
		toolService:            toolService,
		fileService:            fileService,
		imageService:           imageService,
		shortcutRegistry:       shortcutRegistry,
		stateManager:           stateManager,
		messageQueue:           messageQueue,
		agentManager:           agentManager,
		config:                 cfg,
		taskRetentionService:   taskRetentionService,
		backgroundTaskService:  backgroundTaskService,
		backgroundShellService: backgroundShellService,
	}

	handler.messageProcessor = NewChatMessageProcessor(handler)
	handler.shortcutHandler = NewChatShortcutHandler(handler)

	return handler
}

// Handle routes incoming messages to appropriate handler methods based on message type.
// TODO - refactor this
func (h *ChatHandler) Handle(msg tea.Msg) tea.Cmd { // nolint:cyclop,gocyclo,funlen
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
	case domain.BashCommandCompletedEvent:
		return h.HandleBashCommandCompletedEvent(m)
	case domain.BackgroundShellRequestEvent:
		return h.HandleBackgroundShellRequest()
	case domain.ToolExecutionCompletedEvent:
		return h.HandleToolExecutionCompletedEvent(m)
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
	case domain.NavigateBackInTimeEvent:
		return nil
	case domain.MessageHistoryRestoreEvent:
		return nil
	case domain.ComputerUsePausedEvent:
		return h.HandleComputerUsePausedEvent(m)
	case domain.ComputerUseResumedEvent:
		return h.HandleComputerUseResumedEvent(m)
	}

	// No default case - unknown events simply pass through
	// UI events are filtered at the ChatApplication layer via isDomainEvent()
	return nil
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

		ctx = context.WithValue(ctx, domain.ChatHandlerKey, h)

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

// ListenForChatEvents creates a tea.Cmd that listens for the next event from the channel
func (h *ChatHandler) ListenForChatEvents(eventChan <-chan domain.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-eventChan; ok {
			return event
		}
		return nil
	}
}

func (h *ChatHandler) FormatMetrics(metrics *domain.ChatMetrics) string {
	if metrics == nil {
		return ""
	}

	var parts []string

	messages := h.conversationRepo.GetMessages()
	if len(messages) > 0 {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Message.Role == sdk.User {
				actualDuration := time.Since(messages[i].Time).Round(time.Millisecond)
				parts = append(parts, fmt.Sprintf("Time: %v", actualDuration))
				break
			}
		}
	}

	if metrics.Usage != nil {
		if metrics.Usage.PromptTokens > 0 {
			parts = append(parts, fmt.Sprintf("Input: %d tokens", metrics.Usage.PromptTokens))
		}
		if metrics.Usage.CompletionTokens > 0 {
			parts = append(parts, fmt.Sprintf("Output: %d tokens", metrics.Usage.CompletionTokens))
		}
		if metrics.Usage.TotalTokens > 0 {
			parts = append(parts, fmt.Sprintf("Total: %d tokens", metrics.Usage.TotalTokens))
		}
	}

	return strings.Join(parts, " | ")
}

func (h *ChatHandler) ExtractMarkdownSummary(content string) (string, bool) {
	return h.messageProcessor.ExtractMarkdownSummary(content)
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
				Message: fmt.Sprintf("Loaded conversation: %s (%d messages)",
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
	return h.handleChatStart(msg)
}

func (h *ChatHandler) HandleChatChunkEvent(
	msg domain.ChatChunkEvent,
) tea.Cmd {
	return h.handleChatChunk(msg)
}

func (h *ChatHandler) HandleChatCompleteEvent(
	msg domain.ChatCompleteEvent,
) tea.Cmd {
	return h.handleChatComplete(msg)
}

func (h *ChatHandler) HandleChatErrorEvent(
	msg domain.ChatErrorEvent,
) tea.Cmd {
	return h.handleChatError(msg)
}

func (h *ChatHandler) HandleOptimizationStatusEvent(
	msg domain.OptimizationStatusEvent,
) tea.Cmd {
	return h.handleOptimizationStatus(msg)
}

func (h *ChatHandler) HandleToolCallUpdateEvent(
	msg domain.ToolCallUpdateEvent,
) tea.Cmd {
	return h.handleToolCallUpdate(msg)
}

func (h *ChatHandler) HandleToolCallReadyEvent(
	msg domain.ToolCallReadyEvent,
) tea.Cmd {
	return h.handleToolCallReady(msg)
}

func (h *ChatHandler) HandleToolApprovalRequestedEvent(
	msg domain.ToolApprovalRequestedEvent,
) tea.Cmd {
	return h.handleToolApprovalRequested(msg)
}

func (h *ChatHandler) HandleToolExecutionStartedEvent(
	msg domain.ToolExecutionStartedEvent,
) tea.Cmd {
	return h.handleToolExecutionStarted(msg)
}

func (h *ChatHandler) HandleToolExecutionProgressEvent(
	msg domain.ToolExecutionProgressEvent,
) tea.Cmd {
	return h.handleToolExecutionProgress(msg)
}

func (h *ChatHandler) HandleBashOutputChunkEvent(
	msg domain.BashOutputChunkEvent,
) tea.Cmd {
	return h.handleBashOutputChunk(msg)
}

func (h *ChatHandler) HandleBashCommandCompletedEvent(
	msg domain.BashCommandCompletedEvent,
) tea.Cmd {
	return func() tea.Msg {
		return domain.UpdateHistoryEvent(msg)
	}
}

func (h *ChatHandler) HandleToolExecutionCompletedEvent(
	msg domain.ToolExecutionCompletedEvent,
) tea.Cmd {
	return h.handleToolExecutionCompleted(msg)
}

func (h *ChatHandler) HandleCancelledEvent(
	msg domain.CancelledEvent,
) tea.Cmd {
	return h.handleCancelled(msg)
}

func (h *ChatHandler) HandleA2AToolCallExecutedEvent(
	msg domain.A2AToolCallExecutedEvent,
) tea.Cmd {
	return h.handleA2AToolCallExecuted(msg)
}

func (h *ChatHandler) HandleA2ATaskSubmittedEvent(
	msg domain.A2ATaskSubmittedEvent,
) tea.Cmd {
	return h.handleA2ATaskSubmitted(msg)
}

func (h *ChatHandler) HandleA2ATaskStatusUpdateEvent(
	msg domain.A2ATaskStatusUpdateEvent,
) tea.Cmd {
	_, cmd := h.handleA2ATaskStatusUpdate(msg)
	return cmd
}

func (h *ChatHandler) HandleA2ATaskCompletedEvent(
	msg domain.A2ATaskCompletedEvent,
) tea.Cmd {
	_, cmd := h.handleA2ATaskCompleted(msg)
	return cmd
}

func (h *ChatHandler) HandleA2ATaskFailedEvent(
	msg domain.A2ATaskFailedEvent,
) tea.Cmd {
	_, cmd := h.handleA2ATaskFailed(msg)
	return cmd
}

func (h *ChatHandler) HandleA2ATaskInputRequiredEvent(
	msg domain.A2ATaskInputRequiredEvent,
) tea.Cmd {
	_, cmd := h.handleA2ATaskInputRequired(msg, h.stateManager)
	return cmd
}

func (h *ChatHandler) HandleMessageQueuedEvent(
	msg domain.MessageQueuedEvent,
) tea.Cmd {
	_, cmd := h.handleMessageQueued(msg)
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
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

// handleToolApprovalResponse processes the user's approval decision for inline tool approval
func (h *ChatHandler) handleToolApprovalResponse(
	msg domain.ToolApprovalResponseEvent,
) tea.Cmd {
	logger.Info("handleToolApprovalResponse called", "action", msg.Action, "tool", msg.ToolCall.Function.Name)

	if inMemRepo, ok := h.conversationRepo.(*services.InMemoryConversationRepository); ok {
		logger.Info("Updating tool approval status (InMemory)")
		inMemRepo.UpdateToolApprovalStatus(msg.Action)
	} else if persistentRepo, ok := h.conversationRepo.(*services.PersistentConversationRepository); ok {
		logger.Info("Updating tool approval status (Persistent)")
		persistentRepo.UpdateToolApprovalStatus(msg.Action)
	}

	if msg.Action == domain.ApprovalAutoAccept {
		logger.Info("Switching to auto-accept mode for all future tools")
		h.stateManager.SetAgentMode(domain.AgentModeAutoAccept)

		approvalState := h.stateManager.GetApprovalUIState()
		if approvalState != nil && approvalState.ResponseChan != nil {
			select {
			case approvalState.ResponseChan <- domain.ApprovalApprove:
				logger.Info("Sent approval to agent (auto-accept mode)")
			default:
				logger.Warn("Failed to send approval - channel full or closed")
			}
		}

		h.stateManager.ClearApprovalUIState()

		var cmds []tea.Cmd
		cmds = append(cmds, func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: h.conversationRepo.GetMessages(),
			}
		})
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Auto-Approve mode enabled - executing tool...",
				Spinner:    true,
				StatusType: domain.StatusDefault,
				ToolName:   msg.ToolCall.Function.Name,
			}
		})

		if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
			cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
		}

		return tea.Batch(cmds...)
	}

	approvalState := h.stateManager.GetApprovalUIState()
	if approvalState != nil && approvalState.ResponseChan != nil {
		select {
		case approvalState.ResponseChan <- msg.Action:
			logger.Info("Sent approval action to agent", "action", msg.Action)
		default:
			logger.Warn("Failed to send approval - channel full or closed")
		}
	}

	h.stateManager.ClearApprovalUIState()

	var statusMessage string
	var spinner bool
	switch msg.Action {
	case domain.ApprovalApprove:
		statusMessage = fmt.Sprintf("Tool approved - executing %s...", msg.ToolCall.Function.Name)
		spinner = true
	case domain.ApprovalReject:
		statusMessage = fmt.Sprintf("Tool rejected: %s", msg.ToolCall.Function.Name)
		spinner = false
	}

	var cmds []tea.Cmd
	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	})
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMessage,
			Spinner:    spinner,
			StatusType: domain.StatusDefault,
			ToolName:   msg.ToolCall.Function.Name,
		}
	})

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Batch(cmds...)
}

func (h *ChatHandler) HandlePlanApprovalRequestedEvent(
	msg domain.PlanApprovalRequestedEvent,
) tea.Cmd {
	logger.Info("HandlePlanApprovalRequestedEvent called")

	if inMemRepo, ok := h.conversationRepo.(*services.InMemoryConversationRepository); ok {
		logger.Info("Marking last message as plan (InMemory)")
		inMemRepo.MarkLastMessageAsPlan()
	} else if persistentRepo, ok := h.conversationRepo.(*services.PersistentConversationRepository); ok {
		logger.Info("Marking last message as plan (Persistent)")
		persistentRepo.MarkLastMessageAsPlan()
	} else {
		logger.Warn("conversationRepo does not support plan approval", "type", fmt.Sprintf("%T", h.conversationRepo))
	}

	h.stateManager.SetupPlanApprovalUIState(msg.PlanContent, msg.ResponseChan)

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Plan ready - use arrow keys to select and Enter to confirm",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	)
}

func (h *ChatHandler) HandlePlanApprovalResponseEvent(
	msg domain.PlanApprovalResponseEvent,
) tea.Cmd {
	logger.Info("HandlePlanApprovalResponseEvent called", "action", msg.Action)

	planApprovalState := h.stateManager.GetPlanApprovalUIState()
	if planApprovalState == nil {
		logger.Warn("HandlePlanApprovalResponseEvent: planApprovalState is nil, ignoring")
		return nil
	}

	logger.Info("Clearing plan approval UI state to prevent re-entry")
	h.stateManager.ClearPlanApprovalUIState()

	if inMemRepo, ok := h.conversationRepo.(*services.InMemoryConversationRepository); ok {
		logger.Info("Updating plan status (InMemory)")
		inMemRepo.UpdatePlanStatus(msg.Action)
	} else if persistentRepo, ok := h.conversationRepo.(*services.PersistentConversationRepository); ok {
		logger.Info("Updating plan status (Persistent)")
		persistentRepo.UpdatePlanStatus(msg.Action)
	}

	switch msg.Action {
	case domain.PlanApprovalAccept:
		logger.Info("Switching to standard agent mode for plan execution")
		h.stateManager.SetAgentMode(domain.AgentModeStandard)
	case domain.PlanApprovalAcceptAndAutoApprove:
		logger.Info("Switching to auto-accept mode for plan execution")
		h.stateManager.SetAgentMode(domain.AgentModeAutoAccept)
	}

	var statusMessage string
	switch msg.Action {
	case domain.PlanApprovalAccept:
		statusMessage = "Plan accepted - executing plan..."
		logger.Info("Adding hidden continue message to queue")
		h.addHiddenContinueMessage()
	case domain.PlanApprovalReject:
		statusMessage = "Plan rejected - you can provide feedback or changes"
		logger.Info("Ending chat session due to plan rejection")
		h.stateManager.EndChatSession()
	case domain.PlanApprovalAcceptAndAutoApprove:
		statusMessage = "Plan accepted - Auto-Approve mode enabled, executing plan..."
		logger.Info("Adding hidden continue message to queue (auto-approve mode)")
		h.addHiddenContinueMessage()
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMessage,
				Spinner:    msg.Action != domain.PlanApprovalReject,
				StatusType: domain.StatusDefault,
			}
		},
	}

	if msg.Action != domain.PlanApprovalReject {
		logger.Info("Starting new chat session to execute approved plan")
		cmds = append(cmds, h.startChatCompletion())
	}

	return tea.Batch(cmds...)
}

// addHiddenContinueMessage adds a hidden user message to tell the LLM to continue with the approved plan
func (h *ChatHandler) addHiddenContinueMessage() {
	logger.Info("addHiddenContinueMessage called")

	continueMessage := sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent("The plan has been approved. Please proceed with executing it step by step. Start by taking the first action required to implement the plan."),
	}

	entry := domain.ConversationEntry{
		Message: continueMessage,
		Time:    time.Now(),
		Hidden:  true,
	}

	if err := h.conversationRepo.AddMessage(entry); err != nil {
		logger.Error("Failed to add continue message to conversation", "error", err)
		return
	}

	logger.Info("Continue message added to conversation history")
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

// HandleComputerUsePausedEvent handles computer use pause events
func (h *ChatHandler) HandleComputerUsePausedEvent(msg domain.ComputerUsePausedEvent) tea.Cmd {
	logger.Debug("HandleComputerUsePausedEvent called", "request_id", msg.RequestID)
	logger.Info("Computer use execution paused", "request_id", msg.RequestID)

	logger.Debug("Calling agentService.CancelRequest", "request_id", msg.RequestID)
	if err := h.agentService.CancelRequest(msg.RequestID); err != nil {
		logger.Error("Failed to cancel request on pause", "error", err, "request_id", msg.RequestID)
	} else {
		logger.Debug("Successfully cancelled request", "request_id", msg.RequestID)
	}

	logger.Debug("Calling stateManager.SetComputerUsePaused", "request_id", msg.RequestID)
	h.stateManager.SetComputerUsePaused(true, msg.RequestID)

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Computer use paused by user",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}
}

// HandleComputerUseResumedEvent handles computer use resume events
func (h *ChatHandler) HandleComputerUseResumedEvent(msg domain.ComputerUseResumedEvent) tea.Cmd {
	h.stateManager.ClearComputerUsePauseState()

	if h.stateManager.GetChatSession() != nil {
		h.stateManager.EndChatSession()
	}

	continueMessage := sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent("Please continue from where you left off."),
	}

	entry := domain.ConversationEntry{
		Message: continueMessage,
		Time:    time.Now(),
		Hidden:  true,
	}

	if err := h.conversationRepo.AddMessage(entry); err != nil {
		logger.Error("Failed to add continue message", "error", err)
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to resume: %v", err),
				Sticky: false,
			}
		}
	}

	return tea.Batch(
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Resuming execution...",
				Spinner:    true,
				StatusType: domain.StatusDefault,
			}
		},
		h.startChatCompletion(),
	)
}

func (h *ChatHandler) handleChatStart(
	_ /* event */ domain.ChatStartEvent,
) tea.Cmd {
	_ = h.stateManager.UpdateChatStatus(domain.ChatStatusStarting)
	h.activeToolCallID = ""

	var cmds []tea.Cmd
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Starting response...",
			Spinner:    true,
			StatusType: domain.StatusGenerating,
		}
	})

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

func (h *ChatHandler) handleChatChunk(
	msg domain.ChatChunkEvent,
) tea.Cmd {
	chatSession := h.stateManager.GetChatSession()
	if chatSession == nil {
		return h.handleNoChatSession(msg)
	}

	if msg.Content == "" && msg.ReasoningContent == "" {
		return h.handleEmptyContent(chatSession)
	}

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.StreamingContentEvent{
				RequestID: msg.RequestID,
				Content:   msg.Content,
				Delta:     true,
				Model:     chatSession.Model,
			}
		},
	}

	statusCmds := h.handleStatusUpdate(msg, chatSession)
	cmds = append(cmds, statusCmds...)

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

func (h *ChatHandler) handleOptimizationStatus(
	event domain.OptimizationStatusEvent,
) tea.Cmd {
	var cmds []tea.Cmd

	if event.IsActive {
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    event.Message,
				Spinner:    true,
				StatusType: domain.StatusProcessing,
			}
		})
	} else {
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    event.Message,
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		})
	}

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

func (h *ChatHandler) handleNoChatSession(msg domain.ChatChunkEvent) tea.Cmd {
	if msg.ReasoningContent != "" {
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: domain.StatusThinking,
			}
		}
	}
	return nil
}

func (h *ChatHandler) handleEmptyContent(chatSession *domain.ChatSession) tea.Cmd {
	if chatSession != nil && chatSession.EventChannel != nil {
		return h.ListenForChatEvents(chatSession.EventChannel)
	}
	return nil
}

func (h *ChatHandler) handleStatusUpdate(msg domain.ChatChunkEvent, chatSession *domain.ChatSession) []tea.Cmd {
	newStatus, shouldUpdateStatus := h.determineNewStatus(msg, chatSession.Status, chatSession.IsFirstChunk)

	if !shouldUpdateStatus {
		return nil
	}

	_ = h.stateManager.UpdateChatStatus(newStatus)

	if chatSession.IsFirstChunk {
		chatSession.IsFirstChunk = false
		return h.createFirstChunkStatusCmd(newStatus)
	}

	if newStatus != chatSession.Status {
		return h.createStatusUpdateCmd(newStatus)
	}

	return nil
}

func (h *ChatHandler) determineNewStatus(msg domain.ChatChunkEvent, currentStatus domain.ChatStatus, _ bool) (domain.ChatStatus, bool) {
	if msg.ReasoningContent != "" {
		return domain.ChatStatusThinking, true
	}

	if msg.Content != "" {
		return domain.ChatStatusGenerating, true
	}

	return currentStatus, false
}

func (h *ChatHandler) createFirstChunkStatusCmd(status domain.ChatStatus) []tea.Cmd {
	switch status {
	case domain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Thinking...",
				Spinner:    true,
				StatusType: domain.StatusThinking,
			}
		}}
	case domain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Generating response...",
				Spinner:    true,
				StatusType: domain.StatusGenerating,
			}
		}}
	}
	return nil
}

func (h *ChatHandler) createStatusUpdateCmd(status domain.ChatStatus) []tea.Cmd {
	switch status {
	case domain.ChatStatusThinking:
		return []tea.Cmd{func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    "Thinking...",
				StatusType: domain.StatusThinking,
			}
		}}
	case domain.ChatStatusGenerating:
		return []tea.Cmd{func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    "Generating response...",
				StatusType: domain.StatusGenerating,
			}
		}}
	}
	return nil
}

func (h *ChatHandler) handleChatComplete(
	msg domain.ChatCompleteEvent,

) tea.Cmd {
	h.restorePendingModel()

	if len(msg.ToolCalls) == 0 {
		_ = h.stateManager.UpdateChatStatus(domain.ChatStatusCompleted)
	}

	if msg.Message != "" || len(msg.ToolCalls) > 0 {
		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:      sdk.Assistant,
				Content:   sdk.NewMessageContent(msg.Message),
				ToolCalls: &msg.ToolCalls,
			},
			Time: time.Now(),
		}

		if err := h.conversationRepo.AddMessage(assistantEntry); err != nil {
			logger.Error("Failed to add assistant message", "error", err)
		}
	}

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	})

	for _, toolCall := range msg.ToolCalls {
		tc := toolCall
		cmds = append(cmds, func() tea.Msg {
			return domain.ToolCallPreviewEvent{
				RequestID:  msg.RequestID,
				Timestamp:  msg.Timestamp,
				ToolCallID: tc.Id,
				ToolName:   tc.Function.Name,
				Arguments:  tc.Function.Arguments,
				Status:     domain.ToolCallStreamStatusReady,
				IsComplete: false,
			}
		})
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Response complete",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

// restorePendingModel restores the original model if a temporary model switch is pending
func (h *ChatHandler) restorePendingModel() {
	if h.pendingModelRestoration == "" {
		return
	}

	originalModel := h.pendingModelRestoration
	h.pendingModelRestoration = ""

	if err := h.modelService.SelectModel(originalModel); err != nil {
		logger.Error("Failed to restore original model", "model", originalModel, "error", err)
		h.addModelRestorationWarning(originalModel)
		return
	}

	logger.Debug("Successfully restored original model", "model", originalModel)
}

// addModelRestorationWarning adds a warning message when model restoration fails
func (h *ChatHandler) addModelRestorationWarning(originalModel string) {
	warningEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: sdk.NewMessageContent(fmt.Sprintf("[Warning: Failed to restore model to %s]", originalModel)),
		},
		Time: time.Now(),
	}

	if err := h.conversationRepo.AddMessage(warningEntry); err != nil {
		logger.Error("Failed to add model restoration warning message", "error", err)
	}
}

func (h *ChatHandler) handleChatError(
	msg domain.ChatErrorEvent,
) tea.Cmd {
	_ = h.stateManager.UpdateChatStatus(domain.ChatStatusError)
	h.stateManager.EndChatSession()
	h.stateManager.EndToolExecution()
	h.activeToolCallID = ""

	_ = h.stateManager.TransitionToView(domain.ViewStateChat)

	errorMsg := fmt.Sprintf("Chat error: %v", msg.Error)
	if strings.Contains(msg.Error.Error(), "timed out") {
		errorMsg = fmt.Sprintf("⏰ %v\n\nSuggestions:\n• Try breaking your request into smaller parts\n• Check if the server is overloaded\n• Verify your network connection", msg.Error)
	}

	return func() tea.Msg {
		return domain.ShowErrorEvent{
			Error:  errorMsg,
			Sticky: true,
		}
	}
}

func (h *ChatHandler) handleToolCallUpdate(
	msg domain.ToolCallUpdateEvent,

) tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	})

	statusMsg := h.formatToolCallStatusMessage(msg.ToolName, msg.Status)

	switch msg.Status {
	case domain.ToolCallStreamStatusStreaming:
		cmds = append(cmds, func() tea.Msg {
			return domain.UpdateStatusEvent{
				Message:    statusMsg,
				StatusType: domain.StatusWorking,
				ToolName:   msg.ToolName,
			}
		})
	default:
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMsg,
				Spinner:    false,
				StatusType: domain.StatusWorking,
				ToolName:   msg.ToolName,
			}
		})
	}

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

func (h *ChatHandler) handleToolCallReady(
	_ /* msg */ domain.ToolCallReadyEvent,

) tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: h.conversationRepo.GetMessages(),
			}
		},
	}

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

func (h *ChatHandler) handleToolApprovalRequested(
	msg domain.ToolApprovalRequestedEvent,
) tea.Cmd {
	if inMemRepo, ok := h.conversationRepo.(*services.InMemoryConversationRepository); ok {
		if err := inMemRepo.AddPendingToolCall(msg.ToolCall, msg.ResponseChan); err != nil {
			logger.Error("Failed to add pending tool call", "error", err)
		}
	} else if persistentRepo, ok := h.conversationRepo.(*services.PersistentConversationRepository); ok {
		if err := persistentRepo.AddPendingToolCall(msg.ToolCall, msg.ResponseChan); err != nil {
			logger.Error("Failed to add pending tool call", "error", err)
		}
	}

	h.stateManager.SetupApprovalUIState(&msg.ToolCall, msg.ResponseChan)

	h.stateManager.BroadcastEvent(domain.ToolApprovalNotificationEvent{
		RequestID: msg.RequestID,
		Timestamp: time.Now(),
		ToolName:  msg.ToolCall.Function.Name,
		Message:   "Tool approval required - Check terminal for approval",
	})

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Tool approval required: %s", msg.ToolCall.Function.Name),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

func (h *ChatHandler) handleToolExecutionStarted(
	msg domain.ToolExecutionStartedEvent,
) tea.Cmd {
	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Starting tool execution (%d tools)", msg.TotalTools),
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

func (h *ChatHandler) handleToolExecutionProgress(
	msg domain.ToolExecutionProgressEvent,
) tea.Cmd {
	var cmds []tea.Cmd

	// Don't broadcast progress events - tool calls are broadcast from ChatCompleteEvent

	switch msg.Status {
	case "starting":
		h.activeToolCallID = msg.ToolCallID
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    msg.Message,
				Spinner:    true,
				StatusType: domain.StatusWorking,
				ToolName:   msg.ToolName,
			}
		})
	case "running":
		if msg.Message != "" {
			if h.activeToolCallID == msg.ToolCallID {
				cmds = append(cmds, func() tea.Msg {
					return domain.UpdateStatusEvent{
						Message:    msg.Message,
						StatusType: domain.StatusWorking,
						ToolName:   msg.ToolName,
					}
				})
			} else {
				h.activeToolCallID = msg.ToolCallID
				cmds = append(cmds, func() tea.Msg {
					return domain.SetStatusEvent{
						Message:    msg.Message,
						Spinner:    true,
						StatusType: domain.StatusWorking,
						ToolName:   msg.ToolName,
					}
				})
			}
		} else {
			h.activeToolCallID = msg.ToolCallID
		}
	case "completed", "failed":
		h.activeToolCallID = ""
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    msg.Message,
				Spinner:    false,
				StatusType: domain.StatusDefault,
				ToolName:   "",
			}
		})
	case "saving":
		cmds = append(cmds, func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    msg.Message,
				Spinner:    true,
				StatusType: domain.StatusDefault,
				ToolName:   "",
			}
		})
	}

	h.toolEventChannelMu.RLock()
	toolEventChan := h.toolEventChannel
	h.toolEventChannelMu.RUnlock()

	if toolEventChan != nil {
		cmds = append(cmds, h.ListenToToolEvents(toolEventChan))
		return tea.Sequence(cmds...)
	}

	h.bashEventChannelMu.RLock()
	bashEventChan := h.bashEventChannel
	h.bashEventChannelMu.RUnlock()

	if bashEventChan != nil {
		cmds = append(cmds, h.ListenToBashEvents(bashEventChan))
		return tea.Sequence(cmds...)
	}

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	if len(cmds) > 0 {
		return tea.Sequence(cmds...)
	}
	return nil
}

func (h *ChatHandler) handleBashOutputChunk(
	_ domain.BashOutputChunkEvent,
) tea.Cmd {
	h.bashEventChannelMu.RLock()
	bashEventChan := h.bashEventChannel
	h.bashEventChannelMu.RUnlock()

	if bashEventChan != nil {
		return h.ListenToBashEvents(bashEventChan)
	}

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return h.ListenForChatEvents(chatSession.EventChannel)
	}

	return nil
}

func (h *ChatHandler) handleToolExecutionCompleted(
	msg domain.ToolExecutionCompletedEvent,

) tea.Cmd {
	h.activeToolCallID = ""

	cmds := []tea.Cmd{
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message: fmt.Sprintf("Tools completed (%d/%d successful) - preparing response...",
					msg.SuccessCount, msg.TotalExecuted),
				Spinner:    true,
				StatusType: domain.StatusPreparing,
			}
		},
		h.startChatCompletion(),
	}

	todoUpdateCmd := h.extractTodoUpdateCmd(msg.Results)
	if todoUpdateCmd != nil {
		cmds = append(cmds, todoUpdateCmd)
	}

	return tea.Sequence(cmds...)
}

// extractTodoUpdateCmd checks tool results for TodoWrite and returns a command to update todos
func (h *ChatHandler) extractTodoUpdateCmd(results []*domain.ToolExecutionResult) tea.Cmd {
	for _, result := range results {
		if result == nil || result.ToolName != "TodoWrite" || !result.Success {
			continue
		}

		todoResult, ok := result.Data.(*domain.TodoWriteToolResult)
		if !ok || todoResult == nil {
			continue
		}

		todos := todoResult.Todos
		return func() tea.Msg {
			return domain.TodoUpdateEvent{
				Todos: todos,
			}
		}
	}
	return nil
}

func (h *ChatHandler) formatToolCallStatusMessage(toolName string, status domain.ToolCallStreamStatus) string {
	switch status {
	case domain.ToolCallStreamStatusStreaming:
		return fmt.Sprintf("Streaming %s...", toolName)
	case domain.ToolCallStreamStatusComplete:
		return fmt.Sprintf("Completed %s", toolName)
	default:
		return ""
	}
}

func (h *ChatHandler) handleA2ATaskCompleted(
	msg domain.A2ATaskCompletedEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	var taskResult string
	if msg.Result.Data != nil {
		if submitResult, ok := msg.Result.Data.(tools.A2ASubmitTaskResult); ok {
			taskResult = submitResult.TaskResult

			if submitResult.Task != nil && h.taskRetentionService != nil {
				retainedTask := domain.TaskInfo{
					Task:        *submitResult.Task,
					AgentURL:    submitResult.AgentURL,
					StartedAt:   time.Now().Add(-msg.Result.Duration),
					CompletedAt: time.Now(),
				}
				h.taskRetentionService.AddTask(retainedTask)
			}
		}
	}

	if taskResult == "" {
		taskResult = h.conversationRepo.FormatToolResultForLLM(&msg.Result)
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.StreamingContentEvent{
			RequestID: msg.RequestID,
			Content:   taskResult,
			Delta:     false,
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	})

	chatSession := h.stateManager.GetChatSession()

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "A2A task completed",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Sequence(cmds...)
}

func (h *ChatHandler) handleA2ATaskFailed(
	msg domain.A2ATaskFailedEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	var taskResult string
	if msg.Result.Data != nil {
		if submitResult, ok := msg.Result.Data.(tools.A2ASubmitTaskResult); ok {
			taskResult = submitResult.TaskResult

			if submitResult.Task != nil && h.taskRetentionService != nil {
				retainedTask := domain.TaskInfo{
					Task:        *submitResult.Task,
					AgentURL:    submitResult.AgentURL,
					StartedAt:   time.Now().Add(-msg.Result.Duration),
					CompletedAt: time.Now(),
				}
				h.taskRetentionService.AddTask(retainedTask)
			}
		}
	}

	var errorContent string
	if taskResult != "" {
		errorContent = fmt.Sprintf("[A2A Task Failed]\n\n%s", taskResult)
	} else {
		formattedResult := h.conversationRepo.FormatToolResultForLLM(&msg.Result)
		errorContent = fmt.Sprintf("[A2A Task Failed]\n\nError: %s\n\n%s", msg.Error, formattedResult)
	}

	cmds = append(cmds, func() tea.Msg {
		return domain.StreamingContentEvent{
			RequestID: msg.RequestID,
			Content:   errorContent,
			Delta:     false,
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	})

	chatSession := h.stateManager.GetChatSession()

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("A2A task failed: %s", msg.Error),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Sequence(cmds...)
}

func (h *ChatHandler) handleA2ATaskStatusUpdate(
	msg domain.A2ATaskStatusUpdateEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	statusMessage := fmt.Sprintf("A2A task %s: %s", msg.Status, msg.Message)
	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateStatusEvent{
			Message:    statusMessage,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Sequence(cmds...)
}

func (h *ChatHandler) handleMessageQueued(
	_ domain.MessageQueuedEvent,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		return domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Processing queued message...",
			Spinner:    true,
			StatusType: domain.StatusProcessing,
		}
	})

	chatSession := h.stateManager.GetChatSession()
	if chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Sequence(cmds...)
}

func (h *ChatHandler) handleA2ATaskInputRequired(
	msg domain.A2ATaskInputRequiredEvent,
	stateManager domain.StateManager,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	statusMessage := fmt.Sprintf("⚠️  A2A task requires input: %s", msg.Message)
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMessage,
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	})

	if chatSession := stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Sequence(cmds...)
}

func (h *ChatHandler) handleCancelled(
	msg domain.CancelledEvent,
) tea.Cmd {
	_ = h.stateManager.UpdateChatStatus(domain.ChatStatusCancelled)
	h.stateManager.EndChatSession()
	h.stateManager.EndToolExecution()
	h.activeToolCallID = ""

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    fmt.Sprintf("Request cancelled: %s", msg.Reason),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}
}

func (h *ChatHandler) handleA2AToolCallExecuted(
	msg domain.A2AToolCallExecutedEvent,
) tea.Cmd {
	var cmds []tea.Cmd

	statusMessage := fmt.Sprintf("A2A tool %s executed on gateway", msg.ToolName)
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMessage,
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

func (h *ChatHandler) handleA2ATaskSubmitted(
	msg domain.A2ATaskSubmittedEvent,
) tea.Cmd {
	var cmds []tea.Cmd

	statusMessage := fmt.Sprintf("A2A task submitted to %s", msg.AgentName)
	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    statusMessage,
			Spinner:    true,
			StatusType: domain.StatusWorking,
		}
	})

	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return tea.Sequence(cmds...)
}

// SetBashDetachChan sets the bash detach channel (thread-safe)
func (h *ChatHandler) SetBashDetachChan(ch chan<- struct{}) {
	h.bashDetachChanMu.Lock()
	defer h.bashDetachChanMu.Unlock()
	h.bashDetachChan = ch
}

// GetBashDetachChan gets the bash detach channel (thread-safe)
func (h *ChatHandler) GetBashDetachChan() chan<- struct{} {
	h.bashDetachChanMu.RLock()
	defer h.bashDetachChanMu.RUnlock()
	return h.bashDetachChan
}

// ClearBashDetachChan clears the bash detach channel (thread-safe)
func (h *ChatHandler) ClearBashDetachChan() {
	h.bashDetachChanMu.Lock()
	defer h.bashDetachChanMu.Unlock()
	h.bashDetachChan = nil
}

// GetActiveToolCallID returns the currently active tool call ID
func (h *ChatHandler) GetActiveToolCallID() string {
	return h.activeToolCallID
}

// SetActiveToolCallID sets the currently active tool call ID
func (h *ChatHandler) SetActiveToolCallID(id string) {
	h.activeToolCallID = id
}

// HandleCommand processes slash commands
func (h *ChatHandler) HandleCommand(commandText string) tea.Cmd {
	if h.shortcutRegistry == nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Shortcut registry not available",
				Sticky: false,
			}
		}
	}

	mainShortcut, args, err := h.shortcutRegistry.ParseShortcut(commandText)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Invalid shortcut format: %v", err),
				Sticky: false,
			}
		}
	}

	return h.shortcutHandler.executeShortcut(mainShortcut, args)
}

// HandleBashCommand processes bash commands starting with !
func (h *ChatHandler) HandleBashCommand(commandText string) tea.Cmd {
	command := strings.TrimSpace(strings.TrimPrefix(commandText, "!"))

	if command == "" {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No bash command provided. Use: !<command>",
				Sticky: false,
			}
		}
	}

	if strings.HasSuffix(command, " &") || strings.HasSuffix(command, "&") {
		command = strings.TrimSuffix(command, " &")
		command = strings.TrimSuffix(command, "&")
		command = strings.TrimSpace(command)

		if command == "" {
			return func() tea.Msg {
				return domain.ShowErrorEvent{
					Error:  "No bash command provided. Use: !<command>",
					Sticky: false,
				}
			}
		}

		return h.executeBashCommandInBackground(commandText, command)
	}

	return h.executeBashCommand(commandText, command)
}

// HandleToolCommand processes tool commands starting with !!
func (h *ChatHandler) HandleToolCommand(commandText string) tea.Cmd {
	command := strings.TrimSpace(strings.TrimPrefix(commandText, "!!"))

	if command == "" {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No tool command provided. Use: !!ToolName(arg=\"value\")",
				Sticky: false,
			}
		}
	}

	toolName, args, err := h.ParseToolCall(command)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Invalid tool syntax: %v. Use: !!ToolName(arg=\"value\")", err),
				Sticky: false,
			}
		}
	}

	if !h.toolService.IsToolEnabled(toolName) {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Tool '%s' is not enabled. Check 'infer config tools list' for available tools.", toolName),
				Sticky: false,
			}
		}
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to marshal arguments: %v", err),
				Sticky: false,
			}
		}
	}

	return h.executeToolCommand(commandText, toolName, string(argsJSON))
}

// executeBashCommand executes a bash command without approval
func (h *ChatHandler) executeBashCommand(commandText, command string) tea.Cmd {
	toolCallID := fmt.Sprintf("user-bash-%d", time.Now().UnixNano())

	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(commandText),
		},
		Time: time.Now(),
	}
	_ = h.conversationRepo.AddMessage(userEntry)

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Executing: %s", command),
				Spinner:    true,
				StatusType: domain.StatusWorking,
				ToolName:   "Bash",
			}
		},
		h.executeBashCommandAsync(command, toolCallID),
	)
}

// executeBashCommandAsync executes the bash command and returns results
func (h *ChatHandler) executeBashCommandAsync(command string, toolCallID string) tea.Cmd {
	eventChan := make(chan tea.Msg, 10000)
	detachChan := make(chan struct{}, 1)

	h.bashEventChannelMu.Lock()
	h.bashEventChannel = eventChan
	h.bashEventChannelMu.Unlock()

	h.SetBashDetachChan(detachChan)

	go func() {
		defer func() {
			time.Sleep(100 * time.Millisecond)
			close(eventChan)
			h.bashEventChannelMu.Lock()
			h.bashEventChannel = nil
			h.bashEventChannelMu.Unlock()

			h.ClearBashDetachChan()
		}()

		toolCallFunc := sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Bash",
			Arguments: fmt.Sprintf(`{"command": "%s"}`, strings.ReplaceAll(command, `"`, `\"`)),
		}

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			ToolName:   "Bash",
			Arguments:  toolCallFunc.Arguments,
			Status:     "running",
			Message:    "",
		}

		bashCallback := func(line string) {
			eventChan <- domain.BashOutputChunkEvent{
				BaseChatEvent: domain.BaseChatEvent{
					RequestID: toolCallID,
					Timestamp: time.Now(),
				},
				ToolCallID: toolCallID,
				Output:     line,
				IsComplete: false,
			}
		}

		ctx := context.WithValue(context.Background(), domain.ToolApprovedKey, true)
		ctx = context.WithValue(ctx, domain.BashOutputCallbackKey, domain.BashOutputCallback(bashCallback))
		ctx = context.WithValue(ctx, domain.BashDetachChannelKey, (<-chan struct{})(detachChan))
		ctx = context.WithValue(ctx, domain.DirectExecutionKey, true)
		result, err := h.toolService.ExecuteToolDirect(ctx, toolCallFunc)

		if err != nil {
			eventChan <- domain.ToolExecutionProgressEvent{
				BaseChatEvent: domain.BaseChatEvent{
					RequestID: toolCallID,
					Timestamp: time.Now(),
				},
				ToolCallID: toolCallID,
				Status:     "failed",
				Message:    "Execution failed",
			}
			eventChan <- domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to execute command: %v", err),
				Sticky: false,
			}
			return
		}

		status := "completed"
		message := "Completed successfully"
		if result != nil && !result.Success {
			status = "failed"
			message = "Execution failed"
		}

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			Status:     status,
			Message:    message,
		}

		toolCalls := []sdk.ChatCompletionMessageToolCall{
			{
				Id:       toolCallID,
				Type:     "function",
				Function: toolCallFunc,
			},
		}
		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:      sdk.Assistant,
				Content:   sdk.NewMessageContent(""),
				ToolCalls: &toolCalls,
			},
			Time: time.Now(),
		}
		_ = h.conversationRepo.AddMessage(assistantEntry)

		var formattedContent string
		if result != nil {
			formattedContent = h.conversationRepo.FormatToolResultForLLM(result)
		} else {
			formattedContent = "Tool execution failed: no result returned"
		}
		toolEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent(formattedContent),
				ToolCallId: &toolCallID,
			},
			ToolExecution: result,
			Time:          time.Now(),
		}
		_ = h.conversationRepo.AddMessage(toolEntry)

		eventChan <- domain.BashCommandCompletedEvent{
			History: h.conversationRepo.GetMessages(),
		}
	}()

	return h.ListenToBashEvents(eventChan)
}

// ListenToBashEvents listens for bash execution events from the channel
func (h *ChatHandler) ListenToBashEvents(eventChan <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-eventChan
		if !ok {
			return nil
		}
		return msg
	}
}

// executeBashCommandInBackground executes a bash command immediately in the background
func (h *ChatHandler) executeBashCommandInBackground(commandText, command string) tea.Cmd {
	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(commandText),
		},
		Time: time.Now(),
	}
	_ = h.conversationRepo.AddMessage(userEntry)

	go func() {
		ctx := context.WithValue(context.Background(), domain.ToolApprovedKey, true)

		cmd := exec.CommandContext(ctx, "bash", "-c", command)

		outputBuffer := utils.NewOutputRingBuffer(1024 * 1024)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			return
		}

		if err := cmd.Start(); err != nil {
			return
		}

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				line := scanner.Text()
				_, _ = outputBuffer.Write([]byte(line + "\n"))
			}
		}()

		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				_, _ = outputBuffer.Write([]byte(line + "\n"))
			}
		}()

		shellID, err := h.backgroundShellService.DetachToBackground(
			ctx,
			cmd,
			command,
			outputBuffer,
		)

		if err != nil {
			return
		}

		wg.Wait()

		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent(fmt.Sprintf("Command sent to the background with ID: %s. Use ListShells() to view background shells or BashOutput(shell_id=\"%s\") to view output.", shellID, shellID)),
			},
			Time: time.Now(),
		}
		_ = h.conversationRepo.AddMessage(assistantEntry)
	}()

	return tea.Batch(
		func() tea.Msg {
			return domain.UpdateHistoryEvent{
				History: h.conversationRepo.GetMessages(),
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Starting command in background: %s", command),
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	)
}

// HandleBackgroundShellRequest handles a request to background a running bash command
func (h *ChatHandler) HandleBackgroundShellRequest() tea.Cmd {
	detachChan := h.GetBashDetachChan()

	if detachChan == nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "No running Bash command to background",
				Sticky: false,
			}
		}
	}

	select {
	case detachChan <- struct{}{}:
		return func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Moving command to background...",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		}
	default:
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  "Failed to signal detach to running command",
				Sticky: false,
			}
		}
	}
}

// executeToolCommand executes a tool command without approval
func (h *ChatHandler) executeToolCommand(commandText, toolName, argsJSON string) tea.Cmd {
	toolCallID := fmt.Sprintf("user-tool-%d", time.Now().UnixNano())

	userEntry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(commandText),
		},
		Time: time.Now(),
	}
	_ = h.conversationRepo.AddMessage(userEntry)

	return tea.Batch(
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    fmt.Sprintf("Executing: %s", toolName),
				Spinner:    true,
				StatusType: domain.StatusWorking,
				ToolName:   toolName,
			}
		},
		func() tea.Msg {
			return domain.ToolExecutionProgressEvent{
				BaseChatEvent: domain.BaseChatEvent{
					RequestID: toolCallID,
					Timestamp: time.Now(),
				},
				ToolCallID: toolCallID,
				ToolName:   toolName,
				Arguments:  argsJSON,
				Status:     "starting",
				Message:    "",
			}
		},
		h.executeToolCommandAsync(toolName, argsJSON, toolCallID),
	)
}

// executeToolCommandAsync executes the tool command asynchronously and returns results
func (h *ChatHandler) executeToolCommandAsync(toolName, argsJSON, toolCallID string) tea.Cmd {
	eventChan := make(chan tea.Msg, 100)

	h.toolEventChannelMu.Lock()
	h.toolEventChannel = eventChan
	h.toolEventChannelMu.Unlock()

	go func() {
		defer func() {
			time.Sleep(100 * time.Millisecond)
			close(eventChan)
			h.toolEventChannelMu.Lock()
			h.toolEventChannel = nil
			h.toolEventChannelMu.Unlock()
		}()

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			ToolName:   toolName,
			Status:     "running",
			Message:    "Executing...",
		}

		toolCallFunc := sdk.ChatCompletionMessageToolCallFunction{
			Name:      toolName,
			Arguments: argsJSON,
		}

		ctx := context.WithValue(context.Background(), domain.ToolApprovedKey, true)
		ctx = context.WithValue(ctx, domain.DirectExecutionKey, true)
		result, err := h.toolService.ExecuteToolDirect(ctx, toolCallFunc)
		if err != nil {
			eventChan <- domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to execute tool: %v", err),
				Sticky: false,
			}
			return
		}

		toolCalls := []sdk.ChatCompletionMessageToolCall{
			{
				Id:       toolCallID,
				Type:     "function",
				Function: toolCallFunc,
			},
		}
		assistantEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:      sdk.Assistant,
				Content:   sdk.NewMessageContent(""),
				ToolCalls: &toolCalls,
			},
			Time: time.Now(),
		}
		_ = h.conversationRepo.AddMessage(assistantEntry)

		toolEntry := domain.ConversationEntry{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent(""),
				ToolCallId: &toolCallID,
			},
			ToolExecution: result,
			Time:          time.Now(),
		}
		_ = h.conversationRepo.AddMessage(toolEntry)

		status := "completed"
		message := "Completed successfully"
		if result != nil && !result.Success {
			status = "failed"
			message = "Execution failed"
		}

		var images []domain.ImageAttachment
		if result != nil && len(result.Images) > 0 {
			for _, img := range result.Images {
				images = append(images, domain.ImageAttachment{
					Data:        img.Data,
					MimeType:    img.MimeType,
					DisplayName: img.DisplayName,
				})
			}
		}

		eventChan <- domain.ToolExecutionProgressEvent{
			BaseChatEvent: domain.BaseChatEvent{
				RequestID: toolCallID,
				Timestamp: time.Now(),
			},
			ToolCallID: toolCallID,
			ToolName:   toolName,
			Status:     status,
			Message:    message,
			Images:     images,
		}

		eventChan <- domain.UpdateHistoryEvent{
			History: h.conversationRepo.GetMessages(),
		}

		eventChan <- domain.SetStatusEvent{
			Message:    fmt.Sprintf("%s %s", toolName, message),
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}

		// Clear ToolCallRenderer previews now that tool entry is in conversation history
		eventChan <- domain.ChatCompleteEvent{
			RequestID: toolCallID,
			Timestamp: time.Now(),
			Message:   "",
			ToolCalls: []sdk.ChatCompletionMessageToolCall{},
		}
	}()

	return h.ListenToToolEvents(eventChan)
}

// ListenToToolEvents listens for tool execution events from the channel
func (h *ChatHandler) ListenToToolEvents(eventChan <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-eventChan
		if !ok {
			return nil
		}
		return msg
	}
}

// ParseToolCall parses a tool call in the format ToolName(arg="value", arg2="value2") (exposed for testing)
func (h *ChatHandler) ParseToolCall(input string) (string, map[string]any, error) {
	parenIndex := strings.Index(input, "(")
	if parenIndex == -1 {
		return "", nil, fmt.Errorf("missing opening parenthesis")
	}

	toolName := strings.TrimSpace(input[:parenIndex])
	if toolName == "" {
		return "", nil, fmt.Errorf("missing tool name")
	}

	argsStr := strings.TrimSpace(input[parenIndex+1:])
	if !strings.HasSuffix(argsStr, ")") {
		return "", nil, fmt.Errorf("missing closing parenthesis")
	}

	argsStr = strings.TrimSuffix(argsStr, ")")
	argsStr = strings.TrimSpace(argsStr)

	args := make(map[string]any)
	if argsStr == "" {
		return toolName, args, nil
	}

	parsedArgs, err := h.ParseArguments(argsStr)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	return toolName, parsedArgs, nil
}

// ParseArguments parses function arguments in the format key="value", key2="value2" (exposed for testing)
func (h *ChatHandler) ParseArguments(argsStr string) (map[string]any, error) {
	args := make(map[string]any)

	if argsStr == "" {
		return args, nil
	}

	argPattern := regexp.MustCompile(`(\w+)=("[^"]*"|'[^']*'|\w+)`)
	matches := argPattern.FindAllStringSubmatch(argsStr, -1)

	for _, match := range matches {
		if len(match) != 3 {
			continue
		}

		key := match[1]
		value := match[2]

		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
			(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			value = value[1 : len(value)-1]
		}

		if numValue, err := strconv.ParseFloat(value, 64); err == nil {
			args[key] = numValue
		} else {
			args[key] = value
		}
	}

	return args, nil
}
