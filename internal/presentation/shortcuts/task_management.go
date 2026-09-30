package shortcuts

import (
	"context"
)

// A2ATaskManagementShortcut opens the task view: every background job and its
// history, whether or not A2A is enabled.
type A2ATaskManagementShortcut struct{}

// NewA2ATaskManagementShortcut creates the task view shortcut.
func NewA2ATaskManagementShortcut() *A2ATaskManagementShortcut {
	return &A2ATaskManagementShortcut{}
}

func (t *A2ATaskManagementShortcut) GetName() string { return "tasks" }
func (t *A2ATaskManagementShortcut) GetDescription() string {
	return "Show background tasks and their history"
}
func (t *A2ATaskManagementShortcut) GetUsage() string              { return "/tasks" }
func (t *A2ATaskManagementShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (t *A2ATaskManagementShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     "",
		Success:    true,
		SideEffect: SideEffectShowA2ATaskManagement,
	}, nil
}
