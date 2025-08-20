package commands

import (
	"context"
)

// Command interface represents a chat command that can be executed
type Command interface {
	GetName() string
	GetDescription() string
	GetUsage() string
	Execute(ctx context.Context, args []string) (CommandResult, error)
	CanExecute(args []string) bool
}

// CommandResult represents the result of a command execution
type CommandResult struct {
	Output     string
	Success    bool
	SideEffect SideEffectType
	Data       any // Additional data for the side effect
}

// SideEffectType defines the types of side effects a command can have
type SideEffectType int

const (
	SideEffectNone SideEffectType = iota
	SideEffectClearConversation
	SideEffectExportConversation
	SideEffectExit
	SideEffectSwitchModel
	SideEffectShowHistory
	SideEffectShowHelp
)
