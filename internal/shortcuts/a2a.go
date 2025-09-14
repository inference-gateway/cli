package shortcuts

import (
	"context"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

type A2AShortcut struct {
	config          *config.Config
	a2aAgentService domain.A2AAgentService
}

func NewA2AShortcut(cfg *config.Config, a2aAgentService domain.A2AAgentService) *A2AShortcut {
	return &A2AShortcut{
		config:          cfg,
		a2aAgentService: a2aAgentService,
	}
}

func (a *A2AShortcut) GetName() string        { return "a2a" }
func (a *A2AShortcut) GetDescription() string { return "List available A2A agent servers" }
func (a *A2AShortcut) GetUsage() string       { return "/a2a [list]" }
func (a *A2AShortcut) CanExecute(args []string) bool {
	return len(args) == 0 || (len(args) == 1 && args[0] == "list")
}

func (a *A2AShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     "Opening A2A agent servers view...",
		Success:    true,
		SideEffect: SideEffectShowA2AServers,
	}, nil
}

func (a *A2AShortcut) GetA2AAgentService() domain.A2AAgentService {
	return a.a2aAgentService
}
