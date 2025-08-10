package internal

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	fmt.Println("Please select an option:")
	fmt.Println("1. Yes - Execute this command")
	fmt.Println("2. Yes, and don't ask again - Execute this and all future commands")
	fmt.Println("3. No - Cancel command execution")
	fmt.Print("\nEnter your choice (1-3): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return ApprovalDeny, fmt.Errorf("failed to read input: %w", err)
	}

	choice := strings.TrimSpace(input)
	choiceNum, err := strconv.Atoi(choice)
	if err != nil {
		return ApprovalDeny, fmt.Errorf("invalid input: please enter 1, 2, or 3")
	}

	switch choiceNum {
	case 1:
		return ApprovalAllow, nil
	case 2:
		as.skipApproval = true
		return ApprovalAllowAll, nil
	case 3:
		return ApprovalDeny, nil
	default:
		return ApprovalDeny, fmt.Errorf("invalid choice: please enter 1, 2, or 3")
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
