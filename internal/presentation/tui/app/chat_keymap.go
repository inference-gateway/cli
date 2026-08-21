package app

import (
	key "charm.land/bubbles/v2/key"

	config "github.com/inference-gateway/cli/config"
)

// guardKeys holds the fixed key.Bindings for the chat view's precedence
// guards — the focus modes (attachments tree, status bar, question form,
// message history) that capture keys before the keybinding registry runs.
// These are navigation keys local to their overlay and are not user-remappable;
// the config-backed focus-attachments binding lives on ChatApplication.
var guardKeys = struct {
	// interrupt always falls through the guards so the user can cancel the turn.
	interrupt key.Binding

	navUp   key.Binding
	navDown key.Binding
	confirm key.Binding
	cancel  key.Binding

	attachRemove key.Binding
	attachClear  key.Binding
	attachExit   key.Binding

	statusPrev key.Binding
	statusNext key.Binding
	statusHold key.Binding
	statusBlur key.Binding

	questionToggle    key.Binding
	questionBackspace key.Binding
}{
	interrupt: key.NewBinding(key.WithKeys("ctrl+c")),

	navUp:   key.NewBinding(key.WithKeys("up", "k")),
	navDown: key.NewBinding(key.WithKeys("down", "j")),
	confirm: key.NewBinding(key.WithKeys("enter")),
	cancel:  key.NewBinding(key.WithKeys("esc")),

	attachRemove: key.NewBinding(key.WithKeys("d", "x", "backspace", "delete")),
	attachClear:  key.NewBinding(key.WithKeys("c")),
	attachExit:   key.NewBinding(key.WithKeys("esc", "q")),

	statusPrev: key.NewBinding(key.WithKeys("left", "shift+tab")),
	statusNext: key.NewBinding(key.WithKeys("right", "tab")),
	statusHold: key.NewBinding(key.WithKeys("down")),
	statusBlur: key.NewBinding(key.WithKeys("up", "esc")),

	questionToggle:    key.NewBinding(key.WithKeys(" ", "space")),
	questionBackspace: key.NewBinding(key.WithKeys("backspace")),
}

// focusAttachmentsBinding resolves the user-remappable focus-attachments keys
// from the keybindings config (defaults + overrides). A disabled or keyless
// entry yields a binding that never matches.
func focusAttachmentsBinding(kb config.KeybindingsConfig) key.Binding {
	focusKeys := config.ResolveNamespaceBindings(kb, config.NamespaceChat)[actChatFocusAttachments]
	return key.NewBinding(key.WithKeys(focusKeys...))
}
