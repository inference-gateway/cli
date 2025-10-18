package shortcuts

import (
	"context"

	config "github.com/inference-gateway/cli/config"
)

// TaskManagementShortcut shows the task management dropdown
type TaskManagementShortcut struct {
	configService *config.Config
}

// NewTaskManagementShortcut creates a new task management shortcut
func NewTaskManagementShortcut(configService *config.Config) *TaskManagementShortcut {
	return &TaskManagementShortcut{configService: configService}
}

func (t *TaskManagementShortcut) GetName() string { return "tasks" }
func (t *TaskManagementShortcut) GetDescription() string {
	return "Show task management interface"
}
func (t *TaskManagementShortcut) GetUsage() string              { return "/tasks" }
func (t *TaskManagementShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (t *TaskManagementShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	// Check if A2A is enabled
	if !t.configService.A2A.Enabled {
		return ShortcutResult{
			Output:  "❌ Task management requires A2A to be enabled in configuration.",
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:     "",
		Success:    true,
		SideEffect: SideEffectShowTaskManagement,
	}, nil
}
