// TUI state contracts and session state implemented by ApplicationState /
// statemanager.Store: view transitions, the chat and tool-execution
// sessions, and the approval / plan-approval / question prompts. The agent core
// only sees the narrow slices in agent/domain (mode, todos, pause, retry).

package tui

import (
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// ViewNavigator handles view state transitions
type ViewNavigator interface {
	GetCurrentView() ViewState
	GetPreviousView() ViewState
	TransitionToView(newView ViewState) error
}

// ChatSessionState handles chat session lifecycle
type ChatSessionState interface {
	SetChatPending()
	StartChatSession(requestID, model string, eventChan <-chan agentdomain.ChatEvent) error
	UpdateChatStatus(status ChatStatus) error
	EndChatSession()
	GetChatSession() *ChatSession
	IsAgentBusy() bool
	SetRetryStatus(status *agentdomain.RetryStatus)
	GetRetryStatus() *agentdomain.RetryStatus
	TouchChatActivity()
}

// ToolExecutionState handles tool execution sessions
type ToolExecutionState interface {
	StartToolExecution(toolCalls []sdk.ChatCompletionMessageToolCall) error
	CompleteCurrentTool(result *agentdomain.ToolExecutionResult) error
	FailCurrentTool(result *agentdomain.ToolExecutionResult) error
	EndToolExecution()
	GetToolExecution() *ToolExecutionSession
}

// ApprovalPrompt handles tool approval UI state
type ApprovalPrompt interface {
	SetupApprovalUIState(toolCall *sdk.ChatCompletionMessageToolCall, responseChan chan agentdomain.ApprovalAction)
	GetApprovalUIState() *ApprovalUIState
	ClearApprovalUIState()
}

// PlanApprovalPrompt handles plan approval UI state
type PlanApprovalPrompt interface {
	SetupPlanApprovalUIState(planContent, planID string, responseChan chan agentdomain.PlanApprovalAction)
	GetPlanApprovalUIState() *PlanApprovalUIState
	SetPlanApprovalSelectedIndex(index int)
	ClearPlanApprovalUIState()
}

// UserQuestionPrompt handles AskUserQuestion form state
type UserQuestionPrompt interface {
	SetupUserQuestionUIState(questions []agentdomain.UserQuestion, responseChan chan []agentdomain.UserQuestionAnswer)
	GetUserQuestionUIState() *UserQuestionUIState
	ClearUserQuestionUIState()
}

// ChatSession represents an active chat session state
type ChatSession struct {
	RequestID    string
	Status       ChatStatus
	StartTime    time.Time
	Model        string
	EventChannel <-chan agentdomain.ChatEvent
	IsFirstChunk bool
	HasToolCalls bool
	LastActivity time.Time
	RetryStatus  *agentdomain.RetryStatus
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
	Context         string                             `json:"context,omitempty"`
	ResponseChan    chan agentdomain.ApprovalAction    `json:"-"`
}

// PlanApprovalUIState represents the state of plan approval UI
type PlanApprovalUIState struct {
	SelectedIndex int                                 `json:"selected_index"`
	PlanContent   string                              `json:"plan_content"`
	PlanID        string                              `json:"plan_id"`
	ResponseChan  chan agentdomain.PlanApprovalAction `json:"-"`
}

// UserQuestionUIState drives the interactive AskUserQuestion form. The agent
// loop is blocked in the tool goroutine while the form is up; the
// answer-in-progress state lives in the QuestionFormView's huh form.
// ResponseChan delivers the final answers slice back to the blocked tool;
// closing it without a send signals cancellation.
type UserQuestionUIState struct {
	Questions    []agentdomain.UserQuestion            `json:"questions"`
	ResponseChan chan []agentdomain.UserQuestionAnswer `json:"-"`
}

// ChatStatus represents the current chat operation status
type ChatStatus int

const (
	ChatStatusIdle ChatStatus = iota
	ChatStatusStarting
	ChatStatusThinking
	ChatStatusGenerating
	ChatStatusReceivingTools
	ChatStatusWaitingTools
	ChatStatusCompleted
	ChatStatusError
	ChatStatusCancelled
)

func (c ChatStatus) String() string {
	switch c {
	case ChatStatusIdle:
		return "Idle"
	case ChatStatusStarting:
		return "Starting"
	case ChatStatusThinking:
		return "Thinking"
	case ChatStatusGenerating:
		return "Generating"
	case ChatStatusReceivingTools:
		return "ReceivingTools"
	case ChatStatusWaitingTools:
		return "WaitingTools"
	case ChatStatusCompleted:
		return "Completed"
	case ChatStatusError:
		return "Error"
	case ChatStatusCancelled:
		return "Cancelled"
	default:
		return "Unknown"
	}
}

// ToolCall represents a tool call with proper typing
type ToolCall struct {
	ID        string                           `json:"id"`
	Name      string                           `json:"name"`
	Arguments map[string]any                   `json:"arguments"`
	Status    ToolCallStatus                   `json:"status"`
	Result    *agentdomain.ToolExecutionResult `json:"result,omitempty"`
	StartTime time.Time                        `json:"start_time"`
	EndTime   *time.Time                       `json:"end_time,omitempty"`
}

// ToolCallStatus represents the status of an individual tool call
type ToolCallStatus int

const (
	ToolCallStatusPending ToolCallStatus = iota
	ToolCallStatusWaitingApproval
	ToolCallStatusExecuting
	ToolCallStatusCompleted
	ToolCallStatusFailed
	ToolCallStatusCancelled
	ToolCallStatusDenied
)

func (t ToolCallStatus) String() string {
	switch t {
	case ToolCallStatusPending:
		return "Pending"
	case ToolCallStatusWaitingApproval:
		return "WaitingApproval"
	case ToolCallStatusExecuting:
		return "Executing"
	case ToolCallStatusCompleted:
		return "Completed"
	case ToolCallStatusFailed:
		return "Failed"
	case ToolCallStatusCancelled:
		return "Cancelled"
	case ToolCallStatusDenied:
		return "Denied"
	default:
		return "Unknown"
	}
}

// ToolExecutionStatus represents the overall tool execution session status
type ToolExecutionStatus int

const (
	ToolExecutionStatusIdle ToolExecutionStatus = iota
	ToolExecutionStatusProcessing
	ToolExecutionStatusExecuting
	ToolExecutionStatusCompleted
	ToolExecutionStatusFailed
)

func (t ToolExecutionStatus) String() string {
	switch t {
	case ToolExecutionStatusIdle:
		return "Idle"
	case ToolExecutionStatusProcessing:
		return "Processing"
	case ToolExecutionStatusExecuting:
		return "Executing"
	case ToolExecutionStatusCompleted:
		return "Completed"
	case ToolExecutionStatusFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// EventBridgeHolder handles event multicast for external event consumers
type EventBridgeHolder interface {
	SetEventBridge(bridge agentdomain.EventBridge)
	GetEventBridge() agentdomain.EventBridge
	BroadcastEvent(event agentdomain.ChatEvent)
}
