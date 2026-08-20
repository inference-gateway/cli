// State accessor contracts and session state implemented by
// ui.ApplicationState / services.StateManager. The agent core, coordinator
// services, and handlers all consume narrow slices of them, so they live in
// the shared kernel rather than in any single consumer. ViewManager stays in
// internal/ui — view transitions are presentation.

package domain

import (
	"time"

	sdk "github.com/inference-gateway/sdk"
)

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

// ChatSession represents an active chat session state
type ChatSession struct {
	RequestID    string
	Status       ChatStatus
	StartTime    time.Time
	Model        string
	EventChannel <-chan ChatEvent
	IsFirstChunk bool
	HasToolCalls bool
	LastActivity time.Time
	RetryStatus  *RetryStatus
}

// ToolExecutionSession represents an active tool execution session
type ToolExecutionSession struct {
	CurrentTool    *ToolCall
	RemainingTools []ToolCall
	TotalTools     int
	CompletedTools int
	Status         ToolExecutionStatus
	StartTime      time.Time
}

// ApprovalUIState represents the state of approval UI
type ApprovalUIState struct {
	PendingToolCall *sdk.ChatCompletionMessageToolCall `json:"pending_tool_call"`
	ResponseChan    chan ApprovalAction                `json:"-"`
}

// PlanApprovalUIState represents the state of plan approval UI
type PlanApprovalUIState struct {
	SelectedIndex int                     `json:"selected_index"`
	PlanContent   string                  `json:"plan_content"`
	PlanID        string                  `json:"plan_id"`
	ResponseChan  chan PlanApprovalAction `json:"-"`
}

// UserQuestionUIState drives the interactive AskUserQuestion form. The agent
// loop is blocked in the tool goroutine while the form is up; the
// answer-in-progress state lives in the QuestionFormView's huh form.
// ResponseChan delivers the final answers slice back to the blocked tool;
// closing it without a send signals cancellation.
type UserQuestionUIState struct {
	Questions    []UserQuestion            `json:"questions"`
	ResponseChan chan []UserQuestionAnswer `json:"-"`
}
