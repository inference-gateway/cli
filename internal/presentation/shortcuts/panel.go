package shortcuts

import (
	"context"
)

// PanelShortcut is a slash command whose whole job is flipping the TUI to a
// panel view, so an instance differs only in name, description and the side
// effect it emits.
type PanelShortcut struct {
	name        string
	description string
	sideEffect  SideEffectType
}

func (p *PanelShortcut) GetName() string               { return p.name }
func (p *PanelShortcut) GetDescription() string        { return p.description }
func (p *PanelShortcut) GetUsage() string              { return "/" + p.name }
func (p *PanelShortcut) CanExecute(args []string) bool { return len(args) == 0 }

func (p *PanelShortcut) Execute(_ context.Context, _ []string) (ShortcutResult, error) {
	return ShortcutResult{Success: true, SideEffect: p.sideEffect}, nil
}
