package ui

import (
	"github.com/inference-gateway/cli/internal/domain"
)

// UI-specific messages for Bubble Tea

// UpdateHistoryMsg updates the conversation history display
type UpdateHistoryMsg struct {
	History []domain.ConversationEntry
}

// SetStatusMsg sets a status message
type SetStatusMsg struct {
	Message string
	Spinner bool
}

// ShowErrorMsg displays an error message
type ShowErrorMsg struct {
	Error  string
	Sticky bool // Whether error persists until dismissed
}

// ClearErrorMsg clears any displayed error
type ClearErrorMsg struct{}

// UserInputMsg represents user input submission
type UserInputMsg struct {
	Content string
}

// ModelSelectedMsg indicates model selection
type ModelSelectedMsg struct {
	Model string
}

// FileSelectedMsg indicates file selection
type FileSelectedMsg struct {
	FilePath string
}

// FileSelectionRequestMsg requests file selection UI
type FileSelectionRequestMsg struct{}

// CommandSelectedMsg indicates command selection
type CommandSelectedMsg struct {
	Command string
}

// CommandSelectionRequestMsg requests command selection UI
type CommandSelectionRequestMsg struct{}

// ApprovalRequestMsg requests user approval for an action
type ApprovalRequestMsg struct {
	Action      string
	Description string
}

// ApprovalResponseMsg provides approval response
type ApprovalResponseMsg struct {
	Approved   bool
	ApproveAll bool
}

// ScrollRequestMsg requests scrolling in a component
type ScrollRequestMsg struct {
	ComponentID string
	Direction   ScrollDirection
	Amount      int
}

type ScrollDirection int

const (
	ScrollUp ScrollDirection = iota
	ScrollDown
	ScrollToTop
	ScrollToBottom
)

// FocusRequestMsg requests focus change
type FocusRequestMsg struct {
	ComponentID string
}

// ResizeMsg handles terminal resize
type ResizeMsg struct {
	Width  int
	Height int
}
