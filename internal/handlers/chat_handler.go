package handlers

import (
	"time"

	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
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
	skillsService          domain.SkillsService
	githubIssueService     domain.GitHubIssueService
	drainRetryArmed        bool
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
	skillsService domain.SkillsService,
	githubIssueService domain.GitHubIssueService,
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
		skillsService:          skillsService,
		githubIssueService:     githubIssueService,
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
	case domain.RolloverCompletedEvent:
		return h.HandleRolloverCompletedEvent(m)
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
	case domain.SubagentSubmittedEvent:
		return h.HandleSubagentSubmittedEvent(m)
	case domain.SubagentCompletedEvent:
		return h.HandleSubagentCompletedEvent(m)
	case domain.SubagentFailedEvent:
		return h.HandleSubagentFailedEvent(m)
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
	case domain.UserQuestionRequestedEvent:
		return h.HandleUserQuestionRequestedEvent(m)
	case domain.TodoUpdateChatEvent:
		return h.HandleTodoUpdateChatEvent(m)
	case domain.AgentStatusUpdateEvent:
		return h.HandleAgentStatusUpdateEvent(m)
	case domain.DrainQueueEvent:
		return h.HandleDrainQueueEvent(m)
	case domain.DrainQueueRetryEvent:
		return h.HandleDrainQueueRetryEvent(m)
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
	if h.shouldDrainAfterComplete(msg) {
		return tea.Batch(cmd, drainQueueCmd())
	}
	return cmd
}

// shouldDrainAfterComplete reports whether a completed turn should trigger a queue
// drain. A terminal turn - cancelled, or a final answer with no tool calls - is
// the moment queued work should drain (a message typed while busy, or a
// background-job note that landed mid-turn). A turn with tool calls is not
// terminal (results feed back in), so it does not drain.
func (h *ChatHandler) shouldDrainAfterComplete(msg domain.ChatCompleteEvent) bool {
	terminal := msg.Cancelled || len(msg.ToolCalls) == 0
	return terminal && !h.messageQueue.IsEmpty()
}

// drainQueueCmd emits a single DrainQueueEvent on the next loop tick. The gate
// (HandleDrainQueueEvent) starts a fresh turn only if the agent is idle.
func drainQueueCmd() tea.Cmd {
	return func() tea.Msg { return domain.DrainQueueEvent{} }
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

// HandleRolloverCompletedEvent resumes the deferred work after the async
// rollover (kicked off by ChatMessageProcessor.compactThenContinue) finishes.
func (h *ChatHandler) HandleRolloverCompletedEvent(
	msg domain.RolloverCompletedEvent,
) tea.Cmd {
	logger.Info("chat rollover: completed, resuming deferred AddMessage + startChatCompletion",
		"queue_size", h.messageQueue.Size(),
		"repo_messages_before", len(h.conversationRepo.GetMessages()))
	return h.messageProcessor.appendUserMessageAndStartCompletion(msg.Message, msg.Images)
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
	_ domain.MessageQueuedEvent,
) tea.Cmd {
	return h.handleMessageQueued()
}

// Subagent lifecycle events drive the live tree in the conversation view
// (see ConversationView.renderSubagentTree). The handler here only needs to
// keep the chat event listener pumping, since these events arrive on the chat
// event channel and the conversation view consumes them for rendering.
func (h *ChatHandler) HandleSubagentSubmittedEvent(_ domain.SubagentSubmittedEvent) tea.Cmd {
	return h.rearmChatListener()
}

func (h *ChatHandler) HandleSubagentCompletedEvent(_ domain.SubagentCompletedEvent) tea.Cmd {
	return h.rearmChatListener()
}

func (h *ChatHandler) HandleSubagentFailedEvent(_ domain.SubagentFailedEvent) tea.Cmd {
	return h.rearmChatListener()
}

// rearmChatListener re-issues the chat event listener so the Bubble Tea loop
// keeps reading the next event from the channel.
func (h *ChatHandler) rearmChatListener() tea.Cmd {
	if chatSession := h.stateManager.GetChatSession(); chatSession != nil && chatSession.EventChannel != nil {
		return h.ListenForChatEvents(chatSession.EventChannel)
	}
	return nil
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

// HandleUserQuestionRequestedEvent sets up the AskUserQuestion form. The agent
// loop stays blocked in the tool until the user answers, so there is no restart.
func (h *ChatHandler) HandleUserQuestionRequestedEvent(
	msg domain.UserQuestionRequestedEvent,
) tea.Cmd {
	cmd := h.approvalCoordinator.HandleUserQuestionRequested(msg)

	if ch := h.directExec.PendingToolChannel(); ch != nil {
		return tea.Batch(cmd, h.ListenForEvents(ch))
	}
	if cs := h.stateManager.GetChatSession(); cs != nil && cs.EventChannel != nil {
		return tea.Batch(cmd, h.ListenForChatEvents(cs.EventChannel))
	}
	return cmd
}

func (h *ChatHandler) HandlePlanApprovalResponseEvent(
	msg domain.PlanApprovalResponseEvent,
) tea.Cmd {
	planPath := ""
	if st := h.stateManager.GetPlanApprovalUIState(); st != nil {
		planPath = st.PlanPath
	}

	cmd, restart := h.approvalCoordinator.HandlePlanApprovalResponse(msg)
	if !restart {
		return cmd
	}

	if planPath != "" {
		return tea.Batch(cmd, h.newSessionThenExecutePlanCmd(planPath))
	}
	return tea.Batch(cmd, h.startChatCompletion())
}

// HandleAgentStatusUpdateEvent refreshes the agent indicator. The StateManager
// was already updated by the container's status callback before this event was
// pushed, so simply receiving it re-renders the indicator. There is no polling:
// the callback pushes a fresh event on every real status change and stops when
// the agents stop changing.
func (h *ChatHandler) HandleAgentStatusUpdateEvent(_ domain.AgentStatusUpdateEvent) tea.Cmd {
	return nil
}

// HandleDrainQueueEvent gates draining queued work into a fresh agent turn. When
// the chat view is active and the agent is idle, it marks the session pending and
// starts a turn whose CheckingQueue state drains the queue.
//
// When work is stranded (chat view, non-empty queue) it also arms a single bounded
// retry. A background job can finish at the exact instant the agent is still
// finishing its own turn; the supervisor's one-shot DrainQueueEvent would then be
// gate-dropped and, without a retry, the queue would be stranded forever (the old
// always-on ticker's repetition was the de-facto retry). armDrainRetry restores
// that robustness without an idle clock AND without multiplying timers: the
// drainRetryArmed guard keeps exactly one retry chain no matter how many
// DrainQueueEvents arrive, and the chain stops the moment the queue drains (so it
// never fires when idle on chat or off-chat - no /model flicker regression).
//
// SetChatPending() marks the session busy synchronously (StartChatSession only
// runs later inside the async Cmd), so the retry sees "busy" and cannot
// double-start. The Bubble Tea Update loop is single-threaded, so this
// check-then-mark is race-free.
func (h *ChatHandler) HandleDrainQueueEvent(_ domain.DrainQueueEvent) tea.Cmd {
	if h.messageQueue.IsEmpty() || h.stateManager.GetCurrentView() != domain.ViewStateChat {
		return nil
	}

	if h.stateManager.IsAgentBusy() {
		return h.armDrainRetry()
	}

	h.stateManager.SetChatPending()
	return tea.Batch(h.startChatCompletion(), h.armDrainRetry())
}

// armDrainRetry arms ONE bounded retry, collapsing the N-pushes-per-busy-turn case
// to a single timer. It returns nil when a retry is already outstanding, so a burst
// of DrainQueueEvents (one per background job landing in the same busy turn) can
// never spawn parallel retry chains. Race-free: the flag is only ever touched on
// the single-threaded Update loop.
func (h *ChatHandler) armDrainRetry() tea.Cmd {
	if h.drainRetryArmed {
		return nil
	}
	h.drainRetryArmed = true
	return tea.Tick(constants.DrainQueueRetryInterval, func(time.Time) tea.Msg {
		return domain.DrainQueueRetryEvent{}
	})
}

// HandleDrainQueueRetryEvent fires when the bounded retry tick elapses. It clears
// the armed flag (this timer is spent) and re-runs the drain gate, which re-arms a
// single fresh timer iff work is still stranded. A dedicated event type - rather
// than re-emitting DrainQueueEvent - is what lets the guard distinguish "the retry
// fired" from "a fresh external push", so exactly one retry chain stays alive
// regardless of how many DrainQueueEvents were pushed.
func (h *ChatHandler) HandleDrainQueueRetryEvent(_ domain.DrainQueueRetryEvent) tea.Cmd {
	h.drainRetryArmed = false
	return h.HandleDrainQueueEvent(domain.DrainQueueEvent{})
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
