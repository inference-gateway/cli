package domain

import (
	sdk "github.com/inference-gateway/sdk"
)

// UI Events for application state management

// UpdateHistoryEvent updates the conversation history display
type UpdateHistoryEvent struct {
	History []ConversationEntry
}

func (e UpdateHistoryEvent) GetType() UIEventType { return UIEventUpdateHistory }

// SetStatusEvent sets a status message
type SetStatusEvent struct {
	Message    string
	Spinner    bool
	TokenUsage string
	StatusType StatusType
	Progress   *StatusProgress
}

func (e SetStatusEvent) GetType() UIEventType { return UIEventSetStatus }

// UpdateStatusEvent updates an existing status message without resetting timer
type UpdateStatusEvent struct {
	Message    string
	StatusType StatusType
}

func (e UpdateStatusEvent) GetType() UIEventType { return UIEventUpdateStatus }

// ShowErrorEvent displays an error message
type ShowErrorEvent struct {
	Error  string
	Sticky bool // Whether error persists until dismissed
}

func (e ShowErrorEvent) GetType() UIEventType { return UIEventShowError }

// ClearErrorEvent clears any displayed error
type ClearErrorEvent struct{}

func (e ClearErrorEvent) GetType() UIEventType { return UIEventClearError }

// ClearInputEvent clears the input field
type ClearInputEvent struct{}

func (e ClearInputEvent) GetType() UIEventType { return UIEventClearInput }

// SetInputEvent sets text in the input field
type SetInputEvent struct {
	Text string
}

func (e SetInputEvent) GetType() UIEventType { return UIEventSetInput }

// UserInputEvent represents user input submission
type UserInputEvent struct {
	Content string
}

func (e UserInputEvent) GetType() UIEventType { return UIEventUserInput }

// ModelSelectedEvent indicates model selection
type ModelSelectedEvent struct {
	Model string
}

func (e ModelSelectedEvent) GetType() UIEventType { return UIEventModelSelected }

// ThemeSelectedEvent indicates theme selection
type ThemeSelectedEvent struct {
	Theme string
}

func (e ThemeSelectedEvent) GetType() UIEventType { return UIEventThemeSelected }

// ConversationSelectedEvent indicates conversation selection
type ConversationSelectedEvent struct {
	ConversationID string
}

func (e ConversationSelectedEvent) GetType() UIEventType { return UIEventConversationSelected }

// InitializeConversationSelectionEvent indicates conversation selection view should be initialized
type InitializeConversationSelectionEvent struct{}

func (e InitializeConversationSelectionEvent) GetType() UIEventType {
	return UIEventInitializeConversationSelection
}

// FileSelectedEvent indicates file selection
type FileSelectedEvent struct {
	FilePath string
}

func (e FileSelectedEvent) GetType() UIEventType { return UIEventFileSelected }

// FileSelectionRequestEvent requests file selection UI
type FileSelectionRequestEvent struct{}

func (e FileSelectionRequestEvent) GetType() UIEventType { return UIEventFileSelectionRequest }

// SetupFileSelectionEvent sets up file selection state with files
type SetupFileSelectionEvent struct {
	Files []string
}

func (e SetupFileSelectionEvent) GetType() UIEventType { return UIEventSetupFileSelection }

// ApprovalRequestEvent requests user approval for an action
type ApprovalRequestEvent struct {
	Action      string
	Description string
}

func (e ApprovalRequestEvent) GetType() UIEventType { return UIEventApprovalRequest }

// ApprovalResponseEvent provides approval response
type ApprovalResponseEvent struct {
	Approved   bool
	ApproveAll bool
}

func (e ApprovalResponseEvent) GetType() UIEventType { return UIEventApprovalResponse }

// ScrollRequestEvent requests scrolling in a component
type ScrollRequestEvent struct {
	ComponentID string
	Direction   ScrollDirection
	Amount      int
}

func (e ScrollRequestEvent) GetType() UIEventType { return UIEventScrollRequest }

// FocusRequestEvent requests focus change
type FocusRequestEvent struct {
	ComponentID string
}

func (e FocusRequestEvent) GetType() UIEventType { return UIEventFocusRequest }

// ResizeEvent handles terminal resize
type ResizeEvent struct {
	Width  int
	Height int
}

func (e ResizeEvent) GetType() UIEventType { return UIEventResize }

// DebugKeyEvent provides debug information about key presses
type DebugKeyEvent struct {
	Key     string
	Handler string
}

func (e DebugKeyEvent) GetType() UIEventType { return UIEventDebugKey }

// ToggleHelpBarEvent toggles the help bar visibility
type ToggleHelpBarEvent struct{}

func (e ToggleHelpBarEvent) GetType() UIEventType { return UIEventToggleHelpBar }

// HideHelpBarEvent hides the help bar when typing other characters
type HideHelpBarEvent struct{}

func (e HideHelpBarEvent) GetType() UIEventType { return UIEventHideHelpBar }

// ExitSelectionModeEvent exits text selection mode
type ExitSelectionModeEvent struct{}

func (e ExitSelectionModeEvent) GetType() UIEventType { return UIEventExitSelectionMode }

// InitializeTextSelectionEvent initializes text selection mode with current conversation
type InitializeTextSelectionEvent struct{}

func (e InitializeTextSelectionEvent) GetType() UIEventType { return UIEventInitializeTextSelection }

// ConversationsLoadedEvent indicates conversations have been loaded
type ConversationsLoadedEvent struct {
	Conversations []interface{} // Will be cast to ConversationSummary in component
	Error         error
}

func (e ConversationsLoadedEvent) GetType() UIEventType { return UIEventConversationsLoaded }

// Tool Execution Events

// ToolExecutionStartedEvent indicates tool execution has started
type ToolExecutionStartedEvent struct {
	SessionID  string
	TotalTools int
}

func (e ToolExecutionStartedEvent) GetType() UIEventType { return UIEventToolExecutionStarted }

// ToolExecutionProgressEvent indicates progress in tool execution
type ToolExecutionProgressEvent struct {
	SessionID        string
	CurrentTool      int
	TotalTools       int
	ToolName         string
	Status           string
	RequiresApproval bool
}

func (e ToolExecutionProgressEvent) GetType() UIEventType { return UIEventToolExecutionProgress }

// ToolExecutionCompletedEvent indicates tool execution is complete
type ToolExecutionCompletedEvent struct {
	SessionID     string
	TotalExecuted int
	SuccessCount  int
	FailureCount  int
	Results       []*ToolExecutionResult
}

func (e ToolExecutionCompletedEvent) GetType() UIEventType { return UIEventToolExecutionCompleted }

// ToolApprovalRequestEvent requests approval for a specific tool
type ToolApprovalRequestEvent struct {
	SessionID  string
	ToolCall   sdk.ChatCompletionMessageToolCall
	ToolIndex  int
	TotalTools int
}

func (e ToolApprovalRequestEvent) GetType() UIEventType { return UIEventToolApprovalRequest }

// ToolApprovalResponseEvent provides the approval response
type ToolApprovalResponseEvent struct {
	SessionID string
	Approved  bool
	ToolIndex int
}

func (e ToolApprovalResponseEvent) GetType() UIEventType { return UIEventToolApprovalResponse }
