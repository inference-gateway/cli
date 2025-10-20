package shortcuts

import (
	"context"

	config "github.com/inference-gateway/cli/config"
)

// A2ATaskManagementShortcut shows the A2A task management dropdown
type A2ATaskManagementShortcut struct {
	configService *config.Config
}

// NewA2ATaskManagementShortcut creates a new A2A task management shortcut
func NewA2ATaskManagementShortcut(configService *config.Config) *A2ATaskManagementShortcut {
	return &A2ATaskManagementShortcut{configService: configService}
}

func (t *A2ATaskManagementShortcut) GetName() string { return "tasks" }
func (t *A2ATaskManagementShortcut) GetDescription() string {
	return "Show A2A task management interface"
}
func (t *A2ATaskManagementShortcut) GetUsage() string              { return "/tasks" }
func (t *A2ATaskManagementShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (t *A2ATaskManagementShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if !t.configService.A2A.Enabled {
		return ShortcutResult{
			Output:  "Task management requires A2A to be enabled in configuration.",
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:     "",
		Success:    true,
		SideEffect: SideEffectShowA2ATaskManagement,
	}, nil
}
