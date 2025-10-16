package domain

// UI Events for application state management

// UpdateHistoryEvent updates the conversation history display
type UpdateHistoryEvent struct {
	History []ConversationEntry
}


// StreamingContentEvent delivers live streaming content for immediate UI display
type StreamingContentEvent struct {
	RequestID string
	Content   string
	Delta     bool
}


// SetStatusEvent sets a status message
type SetStatusEvent struct {
	Message    string
	Spinner    bool
	TokenUsage string
	StatusType StatusType
	Progress   *StatusProgress
}


// UpdateStatusEvent updates an existing status message without resetting timer
type UpdateStatusEvent struct {
	Message    string
	StatusType StatusType
}


// ShowErrorEvent displays an error message
type ShowErrorEvent struct {
	Error  string
	Sticky bool // Whether error persists until dismissed
}


// ClearErrorEvent clears any displayed error
type ClearErrorEvent struct{}


// ClearInputEvent clears the input field
type ClearInputEvent struct{}


// SetInputEvent sets text in the input field
type SetInputEvent struct {
	Text string
}


// UserInputEvent represents user input submission
type UserInputEvent struct {
	Content string
}


// ModelSelectedEvent indicates model selection
type ModelSelectedEvent struct {
	Model string
}


// ThemeSelectedEvent indicates theme selection
type ThemeSelectedEvent struct {
	Theme string
}


// ConversationSelectedEvent indicates conversation selection
type ConversationSelectedEvent struct {
	ConversationID string
}


// InitializeConversationSelectionEvent indicates conversation selection view should be initialized
type InitializeConversationSelectionEvent struct{}

// FileSelectedEvent indicates file selection
type FileSelectedEvent struct {
	FilePath string
}


// FileSelectionRequestEvent requests file selection UI
type FileSelectionRequestEvent struct{}


// SetupFileSelectionEvent sets up file selection state with files
type SetupFileSelectionEvent struct {
	Files []string
}


// ScrollRequestEvent requests scrolling in a component
type ScrollRequestEvent struct {
	ComponentID string
	Direction   ScrollDirection
	Amount      int
}


// FocusRequestEvent requests focus change
type FocusRequestEvent struct {
	ComponentID string
}


// ResizeEvent handles terminal resize
type ResizeEvent struct {
	Width  int
	Height int
}


// DebugKeyEvent provides debug information about key presses
type DebugKeyEvent struct {
	Key     string
	Handler string
}


// ToggleHelpBarEvent toggles the help bar visibility
type ToggleHelpBarEvent struct{}


// HideHelpBarEvent hides the help bar when typing other characters
type HideHelpBarEvent struct{}


// ExitSelectionModeEvent exits text selection mode
type ExitSelectionModeEvent struct{}


// InitializeTextSelectionEvent initializes text selection mode with current conversation
type InitializeTextSelectionEvent struct{}


// ConversationsLoadedEvent indicates conversations have been loaded
type ConversationsLoadedEvent struct {
	Conversations []interface{}
	Error         error
}


// Tool Execution Events

// ToolExecutionStartedEvent indicates tool execution has started
type ToolExecutionStartedEvent struct {
	SessionID  string
	TotalTools int
}


// ToolExecutionCompletedEvent indicates tool execution is complete
type ToolExecutionCompletedEvent struct {
	SessionID     string
	TotalExecuted int
	SuccessCount  int
	FailureCount  int
	Results       []*ToolExecutionResult
}
