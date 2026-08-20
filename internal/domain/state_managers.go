// State accessor contracts implemented by ApplicationState / services.StateManager.
// They have many consumer packages (handlers, agent, coordinator services, ui), so
// they live here rather than in any single consumer.

package domain

import (
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
	GetAgentMode() AgentMode
	SetAgentMode(mode AgentMode)
	CycleAgentMode() AgentMode
}

// ChatSessionManager handles chat session lifecycle
type ChatSessionManager interface {
	SetChatPending()
	StartChatSession(requestID, model string, eventChan <-chan ChatEvent) error
	UpdateChatStatus(status ChatStatus) error
	EndChatSession()
	GetChatSession() *ChatSession
	IsAgentBusy() bool
	SetRetryStatus(status *RetryStatus)
	GetRetryStatus() *RetryStatus
	TouchChatActivity()
}

// EventBridgeManager handles event multicast for external event consumers
type EventBridgeManager interface {
	SetEventBridge(bridge EventBridge)
	GetEventBridge() EventBridge
	BroadcastEvent(event ChatEvent)
}

// ToolExecutionManager handles tool execution sessions
type ToolExecutionManager interface {
	StartToolExecution(toolCalls []sdk.ChatCompletionMessageToolCall) error
	CompleteCurrentTool(result *ToolExecutionResult) error
	FailCurrentTool(result *ToolExecutionResult) error
	EndToolExecution()
	GetToolExecution() *ToolExecutionSession
}

// ApprovalUIManager handles tool approval UI state
type ApprovalUIManager interface {
	SetupApprovalUIState(toolCall *sdk.ChatCompletionMessageToolCall, responseChan chan ApprovalAction)
	GetApprovalUIState() *ApprovalUIState
	ClearApprovalUIState()
}

// PlanApprovalUIManager handles plan approval UI state
type PlanApprovalUIManager interface {
	SetupPlanApprovalUIState(planContent, planID string, responseChan chan PlanApprovalAction)
	GetPlanApprovalUIState() *PlanApprovalUIState
	SetPlanApprovalSelectedIndex(index int)
	ClearPlanApprovalUIState()
}

// UserQuestionUIManager handles AskUserQuestion form state
type UserQuestionUIManager interface {
	SetupUserQuestionUIState(questions []UserQuestion, responseChan chan []UserQuestionAnswer)
	GetUserQuestionUIState() *UserQuestionUIState
	ClearUserQuestionUIState()
}

// TodoManager handles todo list state
type TodoManager interface {
	SetTodos(todos []TodoItem)
	GetTodos() []TodoItem
}

// ComputerUsePauseManager handles computer use pause state
type ComputerUsePauseManager interface {
	SetComputerUsePaused(paused bool, requestID string)
	IsComputerUsePaused() bool
	GetPausedRequestID() string
	ClearComputerUsePauseState()
}
