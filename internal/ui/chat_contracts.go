package ui

import (
	tea "charm.land/bubbletea/v2"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// ChatHandler defines the interface for the chat handler
// This interface enables testing handlers in isolation and provides a clear contract
type ChatHandler interface {
	// Core event handling (to be deprecated as we move to component-based handling)
	Handle(msg tea.Msg) tea.Cmd

	// Specific event handlers
	HandleUserInputEvent(msg domain.UserInputEvent) tea.Cmd
	HandleFileSelectionRequestEvent(msg domain.FileSelectionRequestEvent) tea.Cmd
	HandleConversationSelectedEvent(msg domain.ConversationSelectedEvent) tea.Cmd
	HandleToolApprovalRequestedEvent(msg agentdomain.ToolApprovalRequestedEvent) tea.Cmd
	HandleToolApprovalResponseEvent(msg domain.ToolApprovalResponseEvent) tea.Cmd
	HandlePlanApprovalRequestedEvent(msg agentdomain.PlanApprovalRequestedEvent) tea.Cmd
	HandlePlanApprovalResponseEvent(msg domain.PlanApprovalResponseEvent) tea.Cmd

	// Command handlers
	HandleCommand(commandText string) tea.Cmd
	HandleBashCommand(commandText string) tea.Cmd
	HandleToolCommand(commandText string) tea.Cmd
	HandleBackgroundShellRequest() tea.Cmd

	// Event channel listeners
	ListenForEvents(eventChan <-chan tea.Msg) tea.Cmd
	ListenForChatEvents(eventChan <-chan agentdomain.ChatEvent) tea.Cmd

	// State management
	GetActiveToolCallID() string
	SetActiveToolCallID(id string)

	// Utility methods
	ParseToolCall(input string) (string, map[string]any, error)
	ParseArguments(argsStr string) (map[string]any, error)
	SetBashDetachChan(chan<- struct{})
	GetBashDetachChan() chan<- struct{}
	ClearBashDetachChan()
}

// ChatEventListener wraps the small "read one message off a channel as a
// tea.Cmd" pattern used throughout the handlers. Extracted so services can
// chain channel reads back into the Bubble Tea event loop without each one
// re-implementing the same closure.
type ChatEventListener interface {
	ListenForChatEvents(eventChan <-chan agentdomain.ChatEvent) tea.Cmd
	ListenForEvents(eventChan <-chan tea.Msg) tea.Cmd
}

// A2ATaskCoordinator owns the UI side of A2A (agent-to-agent) task lifecycle
// events. It translates the six A2A event types into status updates,
// streaming-content events, and conversation-history refreshes, and keeps the
// chat session listener pumping. Self-contained - depends only on the
// conversation repo, task retention, the chat state manager, and a chat
// event listener.
type A2ATaskCoordinator interface {
	HandleTaskSubmitted(msg agentdomain.A2ATaskSubmittedEvent) tea.Cmd
	HandleTaskCompleted(msg agentdomain.A2ATaskCompletedEvent) tea.Cmd
	HandleTaskFailed(msg agentdomain.A2ATaskFailedEvent) tea.Cmd
	HandleTaskStatusUpdate(msg agentdomain.A2ATaskStatusUpdateEvent) tea.Cmd
	HandleTaskInputRequired(msg agentdomain.A2ATaskInputRequiredEvent) tea.Cmd
	HandleToolCallExecuted(msg agentdomain.A2AToolCallExecutedEvent) tea.Cmd
}

// ApprovalCoordinator owns the "pause the assistant turn pending external
// decision" family of events: plan approval (the agent stops, presents a
// plan, awaits user accept/reject) and computer-use pause/resume (the user
// hits a key to interrupt computer-use execution and later resumes).
//
// Response handlers return a restart bool so the orchestrator can fire
// ChatCompletionRunner.Start() after the cmds without ApprovalCoordinator
// having to depend on the runner.
type ApprovalCoordinator interface {
	HandlePlanApprovalRequested(msg agentdomain.PlanApprovalRequestedEvent) tea.Cmd
	HandlePlanApprovalResponse(msg domain.PlanApprovalResponseEvent) (cmd tea.Cmd, restart bool)
	HandleUserQuestionRequested(msg agentdomain.UserQuestionRequestedEvent) tea.Cmd
	HandleComputerUsePaused(msg agentdomain.ComputerUsePausedEvent) tea.Cmd
	HandleComputerUseResumed(msg agentdomain.ComputerUseResumedEvent) (cmd tea.Cmd, restart bool)
}

// ChatCompletionRunner owns the LLM streaming lifecycle - initiating
// streaming, translating chat-start / chat-chunk / chat-complete / chat-error
// events into UI state transitions, and handling the model-restoration side
// effect after a temporary /model switch.
//
// Start takes a BashDetachChannelHolder because the agent core needs that
// narrow interface attached to its context when launching tools that may
// require backgrounding. In #529 commit 3 that holder is the orchestrator
// itself; in commit 4 it becomes the DirectExecutionService.
type ChatCompletionRunner interface {
	Start(holder agentdomain.BashDetachChannelHolder) tea.Cmd
	HandleChatStart(msg agentdomain.ChatStartEvent) tea.Cmd
	HandleChatChunk(msg agentdomain.ChatChunkEvent) tea.Cmd
	HandleChatComplete(msg agentdomain.ChatCompleteEvent) tea.Cmd
	HandleChatError(msg agentdomain.ChatErrorEvent) tea.Cmd
	HandleOptimizationStatus(msg agentdomain.OptimizationStatusEvent) tea.Cmd
	SetPendingRestoration(originalModel string)
}

// ToolExecutionCoordinator owns the tool round-trip: streaming-status updates
// emitted while the model is producing a tool call, approval coordination
// (forwarding the user's decision back to the agent), and execution-progress
// events while the tool runs. Also owns the active-tool-call indicator the
// UI uses to render the in-flight tool name.
type ToolExecutionCoordinator interface {
	GetActiveToolCallID() string
	SetActiveToolCallID(id string)

	HandleToolCallUpdate(msg agentdomain.ToolCallUpdateEvent) tea.Cmd
	HandleToolCallReady(msg agentdomain.ToolCallReadyEvent) tea.Cmd
	HandleToolApprovalRequested(msg agentdomain.ToolApprovalRequestedEvent) tea.Cmd
	HandleToolApprovalResponse(msg domain.ToolApprovalResponseEvent) tea.Cmd
	HandleToolExecutionStarted(msg domain.ToolExecutionStartedEvent) tea.Cmd
	HandleToolExecutionProgress(msg agentdomain.ToolExecutionProgressEvent) tea.Cmd
	HandleToolExecutionCompleted(msg domain.ToolExecutionCompletedEvent) tea.Cmd
	HandleToolCancelled(msg agentdomain.ToolCancelledEvent) tea.Cmd
}

// DirectExecutionService owns user-typed `!command` (bash) and `!!Tool(...)`
// (tool) execution. It synthesizes the conversation entries, spawns the async
// goroutines, owns the per-call event/detach channels, and exposes the
// channels via PendingBashChannel / PendingToolChannel so the
// ToolExecutionCoordinator can keep pumping them.
//
// Implements BashDetachChannelHolder so the agent core can find it on the
// request context (see WithChatHandler / GetChatHandler in
// context_helpers.go).
type DirectExecutionService interface {
	agentdomain.BashDetachChannelHolder

	HandleBashCommand(commandText string) tea.Cmd
	HandleToolCommand(commandText string) tea.Cmd
	HandleBackgroundShellRequest() tea.Cmd
	HandleBashOutputChunk(msg agentdomain.BashOutputChunkEvent) tea.Cmd
	HandleBashCommandCompleted(msg domain.BashCommandCompletedEvent) tea.Cmd

	ParseToolCall(input string) (string, map[string]any, error)
	ParseArguments(argsStr string) (map[string]any, error)

	PendingBashChannel() <-chan tea.Msg
	PendingToolChannel() <-chan tea.Msg
}
