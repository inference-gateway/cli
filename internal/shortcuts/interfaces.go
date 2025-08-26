package shortcuts

import (
	"context"
)

// Shortcut interface represents a chat shortcut that can be executed
type Shortcut interface {
	GetName() string
	GetDescription() string
	GetUsage() string
	Execute(ctx context.Context, args []string) (ShortcutResult, error)
	CanExecute(args []string) bool
}

// ShortcutResult represents the result of a shortcut execution
type ShortcutResult struct {
	Output     string
	Success    bool
	SideEffect SideEffectType
	Data       any
}

// SideEffectType defines the types of side effects a shortcut can have
type SideEffectType int

const (
	SideEffectNone SideEffectType = iota
	SideEffectClearConversation
	SideEffectExportConversation
	SideEffectExit
	SideEffectSwitchModel
	SideEffectShowHelp
	SideEffectReloadConfig
	SideEffectGenerateCommit
	SideEffectResumeConversation
	SideEffectSaveConversation
	SideEffectShowConversationSelection
)
