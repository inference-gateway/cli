package internal

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbletea"
)

// ApprovalSession tracks approval decisions for a session
type ApprovalSession struct {
	skipApproval bool
}

// NewApprovalSession creates a new approval session
func NewApprovalSession() *ApprovalSession {
	return &ApprovalSession{
		skipApproval: false,
	}
}

// ApprovalDecision represents user's approval decision
type ApprovalDecision int

const (
	ApprovalDeny ApprovalDecision = iota
	ApprovalAllow
	ApprovalAllowAll
)

// String returns string representation of approval decision
func (ad ApprovalDecision) String() string {
	switch ad {
	case ApprovalDeny:
		return "No"
	case ApprovalAllow:
		return "Yes"
	case ApprovalAllowAll:
		return "Yes, and don't ask again"
	default:
		return "Unknown"
	}
}

// PromptForApprovalBubbleTea prompts the user for command execution approval using Bubble Tea
func (as *ApprovalSession) PromptForApprovalBubbleTea(command string, program *tea.Program, inputModel *ChatInputModel) (ApprovalDecision, error) {
	if as.skipApproval {
		return ApprovalAllow, nil
	}

	// Send approval request to the chat interface
	program.Send(ApprovalRequestMsg{Command: command})

	// Wait for user response
	for {
		time.Sleep(50 * time.Millisecond)

		if !inputModel.IsApprovalPending() {
			response := inputModel.GetApprovalResponse()
			inputModel.ResetApproval()

			switch response {
			case 1:
				return ApprovalAllow, nil
			case 2:
				as.skipApproval = true
				return ApprovalAllowAll, nil
			case 0:
				return ApprovalDeny, nil
			default:
				return ApprovalDeny, fmt.Errorf("invalid or cancelled selection")
			}
		}
	}
}

// IsApprovalSkipped returns whether approval is being skipped for the session
func (as *ApprovalSession) IsApprovalSkipped() bool {
	return as.skipApproval
}

// SetSkipApproval sets the skip approval flag
func (as *ApprovalSession) SetSkipApproval(skip bool) {
	as.skipApproval = skip
}
