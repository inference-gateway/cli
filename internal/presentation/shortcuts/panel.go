package shortcuts

import (
	"context"
)

// panelShortcut is a slash command whose whole job is flipping the TUI to a
// panel view, so an instance differs only in name, description and the side
// effect it emits.
type panelShortcut struct {
	name        string
	description string
	sideEffect  SideEffectType
}

// newPanelShortcut builds one panel shortcut.
func newPanelShortcut(name, description string, sideEffect SideEffectType) *panelShortcut {
	return &panelShortcut{name: name, description: description, sideEffect: sideEffect}
}

func (p *panelShortcut) GetName() string               { return p.name }
func (p *panelShortcut) GetDescription() string        { return p.description }
func (p *panelShortcut) GetUsage() string              { return "/" + p.name }
func (p *panelShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (p *panelShortcut) Execute(_ context.Context, _ []string) (ShortcutResult, error) {
	return ShortcutResult{Success: true, SideEffect: p.sideEffect}, nil
}
