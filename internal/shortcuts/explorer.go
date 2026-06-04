package shortcuts

import (
	"context"
)

// ExplorerShortcut opens the file explorer panel (VS Code-style tree + fuzzy
// finder). It is a pure view trigger with no dependencies — the side effect
// transitions the UI to the explorer, which walks the working directory itself.
type ExplorerShortcut struct{}

// NewExplorerShortcut creates a new explorer shortcut.
func NewExplorerShortcut() *ExplorerShortcut { return &ExplorerShortcut{} }

func (e *ExplorerShortcut) GetName() string { return "explorer" }
func (e *ExplorerShortcut) GetDescription() string {
	return "Open the file explorer (tree + fuzzy finder)"
}
func (e *ExplorerShortcut) GetUsage() string              { return "/explorer" }
func (e *ExplorerShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (e *ExplorerShortcut) Execute(_ context.Context, _ []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     "",
		Success:    true,
		SideEffect: SideEffectShowExplorer,
	}, nil
}
