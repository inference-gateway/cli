package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
)

type ChatHandler struct {
	agentService           domain.AgentService
	conversationRepo       domain.ConversationRepository
	conversationOptimizer  domain.ConversationOptimizer
	sessionRolloverManager *services.SessionRolloverManager
	modelService           domain.ModelService
	toolService            domain.ToolService
	fileService            domain.FileService
	imageService           domain.ImageService
	shortcutRegistry       *shortcuts.Registry
	stateManager           domain.StateManager
	messageQueue           domain.MessageQueue
	taskRetentionService   domain.TaskRetentionService
	backgroundTaskService  domain.BackgroundTaskService
	backgroundShellService domain.BackgroundShellService
	agentManager           domain.AgentManager
	config                 *config.Config
	a2aTaskCoordinator     domain.A2ATaskCoordinator
	approvalCoordinator    domain.ApprovalCoordinator
	completionRunner       domain.ChatCompletionRunner
	directExec             domain.DirectExecutionService
	toolCoordinator        domain.ToolExecutionCoordinator
	messageProcessor       *ChatMessageProcessor
	shortcutHandler        *ChatShortcutHandler
}

func NewChatHandler(
	agentService domain.AgentService,
	conversationRepo domain.ConversationRepository,
	conversationOptimizer domain.ConversationOptimizer,
	sessionRolloverManager *services.SessionRolloverManager,
	modelService domain.ModelService,
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
	a2aTaskCoordinator domain.A2ATaskCoordinator,
	approvalCoordinator domain.ApprovalCoordinator,
	completionRunner domain.ChatCompletionRunner,
	directExec domain.DirectExecutionService,
	toolCoordinator domain.ToolExecutionCoordinator,
) *ChatHandler {
	handler := &ChatHandler{
		agentService:           agentService,
		conversationRepo:       conversationRepo,
		conversationOptimizer:  conversationOptimizer,
		sessionRolloverManager: sessionRolloverManager,
		modelService:           modelService,
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
		a2aTaskCoordinator:     a2aTaskCoordinator,
		approvalCoordinator:    approvalCoordinator,
		completionRunner:       completionRunner,
		directExec:             directExec,
		toolCoordinator:        toolCoordinator,
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
	case domain.ToolCancelledEvent:
		return h.HandleToolCancelledEvent(m)
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

// startChatCompletion bridges the orchestrator to the extracted runner. The
// DirectExecutionService owns the bash detach channel and satisfies
// BashDetachChannelHolder for the agent core's context lookup.
func (h *ChatHandler) startChatCompletion() tea.Cmd {
	return h.completionRunner.Start(h.directExec)
}

// ListenForChatEvents creates a tea.Cmd that listens for the next event from
// the channel. Wraps the shared eventlistener service so callers within this
// package and the legacy domain.ChatHandler interface continue to work.
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
			history := h.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
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
	h.toolCoordinator.SetActiveToolCallID("")
	return h.completionRunner.HandleChatStart(msg)
}

func (h *ChatHandler) HandleChatChunkEvent(
	msg domain.ChatChunkEvent,
) tea.Cmd {
	return h.completionRunner.HandleChatChunk(msg)
}

func (h *ChatHandler) HandleChatCompleteEvent(
	msg domain.ChatCompleteEvent,
) tea.Cmd {
	cmd := h.completionRunner.HandleChatComplete(msg)
	if msg.Cancelled {
		h.toolCoordinator.SetActiveToolCallID("")
	}
	return cmd
}

func (h *ChatHandler) HandleChatErrorEvent(
	msg domain.ChatErrorEvent,
) tea.Cmd {
	h.toolCoordinator.SetActiveToolCallID("")
	return h.completionRunner.HandleChatError(msg)
}

func (h *ChatHandler) HandleOptimizationStatusEvent(
	msg domain.OptimizationStatusEvent,
) tea.Cmd {
	return h.completionRunner.HandleOptimizationStatus(msg)
}

func (h *ChatHandler) HandleToolCallUpdateEvent(
	msg domain.ToolCallUpdateEvent,
) tea.Cmd {
	return h.toolCoordinator.HandleToolCallUpdate(msg)
}

func (h *ChatHandler) HandleToolCallReadyEvent(
	msg domain.ToolCallReadyEvent,
) tea.Cmd {
	return h.toolCoordinator.HandleToolCallReady(msg)
}

func (h *ChatHandler) HandleToolApprovalRequestedEvent(
	msg domain.ToolApprovalRequestedEvent,
) tea.Cmd {
	return h.toolCoordinator.HandleToolApprovalRequested(msg)
}

func (h *ChatHandler) HandleToolExecutionStartedEvent(
	msg domain.ToolExecutionStartedEvent,
) tea.Cmd {
	return h.toolCoordinator.HandleToolExecutionStarted(msg)
}

func (h *ChatHandler) HandleToolExecutionProgressEvent(
	msg domain.ToolExecutionProgressEvent,
) tea.Cmd {
	return h.toolCoordinator.HandleToolExecutionProgress(msg)
}

func (h *ChatHandler) HandleBashOutputChunkEvent(
	msg domain.BashOutputChunkEvent,
) tea.Cmd {
	return h.directExec.HandleBashOutputChunk(msg)
}

func (h *ChatHandler) HandleBashCommandCompletedEvent(
	msg domain.BashCommandCompletedEvent,
) tea.Cmd {
	return h.directExec.HandleBashCommandCompleted(msg)
}

func (h *ChatHandler) HandleToolExecutionCompletedEvent(
	msg domain.ToolExecutionCompletedEvent,
) tea.Cmd {
	return h.toolCoordinator.HandleToolExecutionCompleted(msg)
}

func (h *ChatHandler) HandleA2AToolCallExecutedEvent(
	msg domain.A2AToolCallExecutedEvent,
) tea.Cmd {
	return h.a2aTaskCoordinator.HandleToolCallExecuted(msg)
}

func (h *ChatHandler) HandleA2ATaskSubmittedEvent(
	msg domain.A2ATaskSubmittedEvent,
) tea.Cmd {
	return h.a2aTaskCoordinator.HandleTaskSubmitted(msg)
}

func (h *ChatHandler) HandleA2ATaskStatusUpdateEvent(
	msg domain.A2ATaskStatusUpdateEvent,
) tea.Cmd {
	return h.a2aTaskCoordinator.HandleTaskStatusUpdate(msg)
}

func (h *ChatHandler) HandleA2ATaskCompletedEvent(
	msg domain.A2ATaskCompletedEvent,
) tea.Cmd {
	return h.a2aTaskCoordinator.HandleTaskCompleted(msg)
}

func (h *ChatHandler) HandleA2ATaskFailedEvent(
	msg domain.A2ATaskFailedEvent,
) tea.Cmd {
	return h.a2aTaskCoordinator.HandleTaskFailed(msg)
}

func (h *ChatHandler) HandleA2ATaskInputRequiredEvent(
	msg domain.A2ATaskInputRequiredEvent,
) tea.Cmd {
	return h.a2aTaskCoordinator.HandleTaskInputRequired(msg)
}

func (h *ChatHandler) HandleMessageQueuedEvent(
	msg domain.MessageQueuedEvent,
) tea.Cmd {
	_, cmd := h.handleMessageQueued(msg)
	return cmd
}

func (h *ChatHandler) HandleToolCancelledEvent(
	msg domain.ToolCancelledEvent,
) tea.Cmd {
	return h.toolCoordinator.HandleToolCancelled(msg)
}

func (h *ChatHandler) HandleToolApprovalResponseEvent(
	msg domain.ToolApprovalResponseEvent,
) tea.Cmd {
	return h.toolCoordinator.HandleToolApprovalResponse(msg)
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

func (h *ChatHandler) HandlePlanApprovalRequestedEvent(
	msg domain.PlanApprovalRequestedEvent,
) tea.Cmd {
	return h.approvalCoordinator.HandlePlanApprovalRequested(msg)
}

func (h *ChatHandler) HandlePlanApprovalResponseEvent(
	msg domain.PlanApprovalResponseEvent,
) tea.Cmd {
	cmd, restart := h.approvalCoordinator.HandlePlanApprovalResponse(msg)
	if !restart {
		return cmd
	}
	return tea.Batch(cmd, h.startChatCompletion())
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
	return h.approvalCoordinator.HandleComputerUsePaused(msg)
}

// HandleComputerUseResumedEvent handles computer use resume events
func (h *ChatHandler) HandleComputerUseResumedEvent(msg domain.ComputerUseResumedEvent) tea.Cmd {
	cmd, restart := h.approvalCoordinator.HandleComputerUseResumed(msg)
	if !restart {
		return cmd
	}
	return tea.Batch(cmd, h.startChatCompletion())
}

func (h *ChatHandler) handleMessageQueued(
	_ domain.MessageQueuedEvent,
) (tea.Model, tea.Cmd) {
	chatSession := h.stateManager.GetChatSession()

	var cmds []tea.Cmd

	cmds = append(cmds, func() tea.Msg {
		history := h.conversationRepo.GetMessages()
		return domain.UpdateHistoryEvent{
			History: history,
		}
	})

	cmds = append(cmds, func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Processing queued message...",
			Spinner:    true,
			StatusType: domain.StatusProcessing,
		}
	})

	if chatSession != nil && chatSession.EventChannel != nil {
		cmds = append(cmds, h.ListenForChatEvents(chatSession.EventChannel))
	}

	return nil, tea.Sequence(cmds...)
}

// SetBashDetachChan satisfies the legacy domain.ChatHandler interface by
// forwarding to DirectExecutionService (the actual owner post-#529).
func (h *ChatHandler) SetBashDetachChan(ch chan<- struct{}) {
	h.directExec.SetBashDetachChan(ch)
}

// GetBashDetachChan satisfies the legacy domain.ChatHandler interface by
// forwarding to DirectExecutionService.
func (h *ChatHandler) GetBashDetachChan() chan<- struct{} {
	return h.directExec.GetBashDetachChan()
}

// ClearBashDetachChan satisfies the legacy domain.ChatHandler interface by
// forwarding to DirectExecutionService.
func (h *ChatHandler) ClearBashDetachChan() {
	h.directExec.ClearBashDetachChan()
}

// GetActiveToolCallID satisfies the legacy domain.ChatHandler interface by
// forwarding to ToolExecutionCoordinator (the actual owner post-#529).
func (h *ChatHandler) GetActiveToolCallID() string {
	return h.toolCoordinator.GetActiveToolCallID()
}

// SetActiveToolCallID satisfies the legacy domain.ChatHandler interface by
// forwarding to ToolExecutionCoordinator.
func (h *ChatHandler) SetActiveToolCallID(id string) {
	h.toolCoordinator.SetActiveToolCallID(id)
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

// HandleBashCommand processes bash commands starting with !. Delegates to
// DirectExecutionService.
func (h *ChatHandler) HandleBashCommand(commandText string) tea.Cmd {
	return h.directExec.HandleBashCommand(commandText)
}

// HandleToolCommand processes tool commands starting with !!. Delegates to
// DirectExecutionService.
func (h *ChatHandler) HandleToolCommand(commandText string) tea.Cmd {
	return h.directExec.HandleToolCommand(commandText)
}

// HandleBackgroundShellRequest signals the currently-running bash command to
// detach to the background. Delegates to DirectExecutionService.
func (h *ChatHandler) HandleBackgroundShellRequest() tea.Cmd {
	return h.directExec.HandleBackgroundShellRequest()
}

// ParseToolCall satisfies the legacy domain.ChatHandler interface by
// delegating to DirectExecutionService.
func (h *ChatHandler) ParseToolCall(input string) (string, map[string]any, error) {
	return h.directExec.ParseToolCall(input)
}

// ParseArguments satisfies the legacy domain.ChatHandler interface by
// delegating to DirectExecutionService.
func (h *ChatHandler) ParseArguments(argsStr string) (map[string]any, error) {
	return h.directExec.ParseArguments(argsStr)
}

// ListenForEvents reads one event off a generic tea.Msg channel as a tea.Cmd.
// Kept on the orchestrator because domain.ChatHandler still exposes it and
// the runner-internal handlers (tool execution progress) call it directly.
func (h *ChatHandler) ListenForEvents(eventChan <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-eventChan
		if !ok {
			return nil
		}
		return msg
	}
}
