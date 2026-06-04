package shortcuts

import (
	"context"
)

// DiffShortcut opens the changes panel (interactive diff viewer). It is a pure
// view trigger with no dependencies — the side effect transitions the UI to the
// diff viewer, which loads working-tree changes itself.
type DiffShortcut struct{}

// NewDiffShortcut creates a new diff shortcut.
func NewDiffShortcut() *DiffShortcut { return &DiffShortcut{} }

func (d *DiffShortcut) GetName() string { return "diff" }
func (d *DiffShortcut) GetDescription() string {
	return "Open the changes panel (interactive diff viewer)"
}
func (d *DiffShortcut) GetUsage() string              { return "/diff" }
func (d *DiffShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (d *DiffShortcut) Execute(_ context.Context, _ []string) (ShortcutResult, error) {
	return ShortcutResult{
		Output:     "",
		Success:    true,
		SideEffect: SideEffectShowDiffViewer,
	}, nil
}
