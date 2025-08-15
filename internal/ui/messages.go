package ui

import (
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// Re-export shared message types for backward compatibility
type UpdateHistoryMsg = shared.UpdateHistoryMsg
type SetStatusMsg = shared.SetStatusMsg
type ShowErrorMsg = shared.ShowErrorMsg
type ClearErrorMsg = shared.ClearErrorMsg
type ClearInputMsg = shared.ClearInputMsg
type SetInputMsg = shared.SetInputMsg
type UserInputMsg = shared.UserInputMsg
type ModelSelectedMsg = shared.ModelSelectedMsg
type FileSelectedMsg = shared.FileSelectedMsg
type FileSelectionRequestMsg = shared.FileSelectionRequestMsg
type ApprovalRequestMsg = shared.ApprovalRequestMsg
type ApprovalResponseMsg = shared.ApprovalResponseMsg
type ScrollRequestMsg = shared.ScrollRequestMsg
type FocusRequestMsg = shared.FocusRequestMsg
type ResizeMsg = shared.ResizeMsg
type DebugKeyMsg = shared.DebugKeyMsg
type ToggleHelpBarMsg = shared.ToggleHelpBarMsg
type HideHelpBarMsg = shared.HideHelpBarMsg

// Re-export shared types
type ScrollDirection = shared.ScrollDirection

const (
	ScrollUp       = shared.ScrollUp
	ScrollDown     = shared.ScrollDown
	ScrollToTop    = shared.ScrollToTop
	ScrollToBottom = shared.ScrollToBottom
)
