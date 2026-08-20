// State accessor contracts implemented by ApplicationState / services.StateManager.
// They have many consumer packages (handlers, agent, coordinator services, ui), so
// they live here rather than in any single consumer.

package domain

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ViewManager handles view state transitions
type ViewManager interface {
	GetCurrentView() ViewState
	GetPreviousView() ViewState
	TransitionToView(newView ViewState) error
}

// AgentModeManager handles agent mode switching
type AgentModeManager interface {
	GetAgentMode() agentdomain.AgentMode
	SetAgentMode(mode agentdomain.AgentMode)
	CycleAgentMode() agentdomain.AgentMode
}

// ChatSessionManager handles chat session lifecycle
type ChatSessionManager interface {
	SetChatPending()
	StartChatSession(requestID, model string, eventChan <-chan agentdomain.ChatEvent) error
	UpdateChatStatus(status agentdomain.ChatStatus) error
	EndChatSession()
	GetChatSession() *ChatSession
	IsAgentBusy() bool
	SetRetryStatus(status *agentdomain.RetryStatus)
	GetRetryStatus() *agentdomain.RetryStatus
	TouchChatActivity()
}

// EventBridgeManager handles event multicast for external event consumers
type EventBridgeManager interface {
	SetEventBridge(bridge agentdomain.EventBridge)
	GetEventBridge() agentdomain.EventBridge
	BroadcastEvent(event agentdomain.ChatEvent)
}

// ToolExecutionManager handles tool execution sessions
type ToolExecutionManager interface {
	StartToolExecution(toolCalls []sdk.ChatCompletionMessageToolCall) error
	CompleteCurrentTool(result *agentdomain.ToolExecutionResult) error
	FailCurrentTool(result *agentdomain.ToolExecutionResult) error
	EndToolExecution()
	GetToolExecution() *ToolExecutionSession
}

// ApprovalUIManager handles tool approval UI state
type ApprovalUIManager interface {
	SetupApprovalUIState(toolCall *sdk.ChatCompletionMessageToolCall, responseChan chan agentdomain.ApprovalAction)
	GetApprovalUIState() *ApprovalUIState
	ClearApprovalUIState()
}

// PlanApprovalUIManager handles plan approval UI state
type PlanApprovalUIManager interface {
	SetupPlanApprovalUIState(planContent, planID string, responseChan chan agentdomain.PlanApprovalAction)
	GetPlanApprovalUIState() *PlanApprovalUIState
	SetPlanApprovalSelectedIndex(index int)
	ClearPlanApprovalUIState()
}

// UserQuestionUIManager handles AskUserQuestion form state
type UserQuestionUIManager interface {
	SetupUserQuestionUIState(questions []agentdomain.UserQuestion, responseChan chan []agentdomain.UserQuestionAnswer)
	GetUserQuestionUIState() *UserQuestionUIState
	ClearUserQuestionUIState()
}

// TodoManager handles todo list state
type TodoManager interface {
	SetTodos(todos []agentdomain.TodoItem)
	GetTodos() []agentdomain.TodoItem
}

// ComputerUsePauseManager handles computer use pause state
type ComputerUsePauseManager interface {
	SetComputerUsePaused(paused bool, requestID string)
	IsComputerUsePaused() bool
	GetPausedRequestID() string
	ClearComputerUsePauseState()
}
