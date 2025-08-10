package internal

import (
	"fmt"

	"github.com/manifoldco/promptui"
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

// PromptForApproval prompts the user for command execution approval
func (as *ApprovalSession) PromptForApproval(command string) (ApprovalDecision, error) {
	if as.skipApproval {
		return ApprovalAllow, nil
	}

	fmt.Printf("\n⚠️  Command execution approval required:\n")
	fmt.Printf("Command: %s\n\n", command)

	options := []string{
		"Yes - Execute this command",
		"Yes, and don't ask again - Execute this and all future commands",
		"No - Cancel command execution",
	}

	prompt := promptui.Select{
		Label: "Please select an option",
		Items: options,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ . }}:",
			Active:   "▶ {{ . | cyan | bold }}",
			Inactive: "  {{ . }}",
			Selected: "✓ {{ . | green }}",
		},
	}

	index, _, err := prompt.Run()
	if err != nil {
		return ApprovalDeny, fmt.Errorf("selection failed: %w", err)
	}

	switch index {
	case 0:
		return ApprovalAllow, nil
	case 1:
		as.skipApproval = true
		return ApprovalAllowAll, nil
	case 2:
		return ApprovalDeny, nil
	default:
		return ApprovalDeny, fmt.Errorf("invalid selection")
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
