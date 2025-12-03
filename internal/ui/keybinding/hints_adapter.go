package keybinding

import (
	hints "github.com/inference-gateway/cli/internal/ui/hints"
)

// keyActionAdapter adapts KeyAction to hints.KeyAction interface
type keyActionAdapter struct {
	*KeyAction
}

func (a *keyActionAdapter) GetKeys() []string {
	return a.Keys
}

func (a *keyActionAdapter) IsEnabled() bool {
	return a.Enabled
}

// registryAdapter adapts Registry to hints.KeyRegistry interface
type registryAdapter struct {
	*Registry
}

func (r *registryAdapter) GetAction(id string) hints.KeyAction {
	action := r.Registry.GetAction(id)
	if action == nil {
		return nil
	}
	return &keyActionAdapter{action}
}

// NewHintFormatterFromRegistry creates a hints.Formatter from a keybinding Registry
func NewHintFormatterFromRegistry(registry *Registry) *hints.Formatter {
	adapter := &registryAdapter{Registry: registry}
	return hints.NewFormatter(adapter)
}
