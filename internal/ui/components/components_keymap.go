package components

import (
	key "charm.land/bubbles/v2/key"
)

// Centralised key.Binding sets for per-view components. These are navigation
// keys local to their overlay and are not user-remappable; each set follows
// the guardKeys pattern from internal/app/chat_keymap.go.

var taskManagerKeys = struct {
	quit      key.Binding
	enter     key.Binding
	navUp     key.Binding
	navDown   key.Binding
	info      key.Binding
	cancel    key.Binding
	search    key.Binding
	escape    key.Binding
	tab1      key.Binding
	tab2      key.Binding
	tab3      key.Binding
	tab4      key.Binding
	tab5      key.Binding
	confirm   key.Binding
	deny      key.Binding
	close     key.Binding
	pgUp      key.Binding
	pgDown    key.Binding
	top       key.Binding
	bottom    key.Binding
	backspace key.Binding
}{
	quit:      key.NewBinding(key.WithKeys("q", "esc", "ctrl+c")),
	enter:     key.NewBinding(key.WithKeys("enter")),
	navUp:     key.NewBinding(key.WithKeys("up", "k")),
	navDown:   key.NewBinding(key.WithKeys("down", "j")),
	info:      key.NewBinding(key.WithKeys("i")),
	cancel:    key.NewBinding(key.WithKeys("c")),
	search:    key.NewBinding(key.WithKeys("/")),
	escape:    key.NewBinding(key.WithKeys("esc")),
	tab1:      key.NewBinding(key.WithKeys("1")),
	tab2:      key.NewBinding(key.WithKeys("2")),
	tab3:      key.NewBinding(key.WithKeys("3")),
	tab4:      key.NewBinding(key.WithKeys("4")),
	tab5:      key.NewBinding(key.WithKeys("5")),
	confirm:   key.NewBinding(key.WithKeys("y", "Y")),
	deny:      key.NewBinding(key.WithKeys("n", "N", "esc")),
	close:     key.NewBinding(key.WithKeys("q", "esc", "i", "ctrl+c")),
	pgUp:      key.NewBinding(key.WithKeys("pgup", "b")),
	pgDown:    key.NewBinding(key.WithKeys("pgdown", "f")),
	top:       key.NewBinding(key.WithKeys("g")),
	bottom:    key.NewBinding(key.WithKeys("G")),
	backspace: key.NewBinding(key.WithKeys("backspace")),
}

var modelSelectorKeys = struct {
	cancel    key.Binding
	tab1      key.Binding
	tab2      key.Binding
	tab3      key.Binding
	tab4      key.Binding
	search    key.Binding
	enter     key.Binding
	navUp     key.Binding
	navDown   key.Binding
	escape    key.Binding
	backspace key.Binding
}{
	cancel:    key.NewBinding(key.WithKeys("ctrl+c")),
	tab1:      key.NewBinding(key.WithKeys("1")),
	tab2:      key.NewBinding(key.WithKeys("2")),
	tab3:      key.NewBinding(key.WithKeys("3")),
	tab4:      key.NewBinding(key.WithKeys("4")),
	search:    key.NewBinding(key.WithKeys("/")),
	enter:     key.NewBinding(key.WithKeys("enter")),
	navUp:     key.NewBinding(key.WithKeys("up")),
	navDown:   key.NewBinding(key.WithKeys("down")),
	escape:    key.NewBinding(key.WithKeys("esc")),
	backspace: key.NewBinding(key.WithKeys("backspace")),
}

var conversationSelectorKeys = struct {
	cancel    key.Binding
	enter     key.Binding
	search    key.Binding
	delete    key.Binding
	backspace key.Binding
	confirm   key.Binding
	deny      key.Binding
}{
	cancel:    key.NewBinding(key.WithKeys("ctrl+c", "esc")),
	enter:     key.NewBinding(key.WithKeys("enter")),
	search:    key.NewBinding(key.WithKeys("/")),
	delete:    key.NewBinding(key.WithKeys("d", "delete")),
	backspace: key.NewBinding(key.WithKeys("backspace")),
	confirm:   key.NewBinding(key.WithKeys("y", "Y")),
	deny:      key.NewBinding(key.WithKeys("n", "N", "esc")),
}

var helpViewKeys = struct {
	dismiss key.Binding
	navUp   key.Binding
	navDown key.Binding
	pgUp    key.Binding
	pgDown  key.Binding
	top     key.Binding
	bottom  key.Binding
}{
	dismiss: key.NewBinding(key.WithKeys("esc", "q", "ctrl+c")),
	navUp:   key.NewBinding(key.WithKeys("up", "k")),
	navDown: key.NewBinding(key.WithKeys("down", "j")),
	pgUp:    key.NewBinding(key.WithKeys("pgup", "b")),
	pgDown:  key.NewBinding(key.WithKeys("pgdown", "f")),
	top:     key.NewBinding(key.WithKeys("home", "g")),
	bottom:  key.NewBinding(key.WithKeys("end", "G")),
}

// listViewKeys is shared by a2a_agents, tools, and theme selection views.
var listViewKeys = struct {
	cancel    key.Binding
	esc       key.Binding
	selectKey key.Binding
}{
	cancel:    key.NewBinding(key.WithKeys("ctrl+c")),
	esc:       key.NewBinding(key.WithKeys("esc")),
	selectKey: key.NewBinding(key.WithKeys("enter")),
}

var fileSelectionKeys = struct {
	navUp     key.Binding
	navDown   key.Binding
	selectKey key.Binding
	backspace key.Binding
	cancel    key.Binding
}{
	navUp:     key.NewBinding(key.WithKeys("up")),
	navDown:   key.NewBinding(key.WithKeys("down")),
	selectKey: key.NewBinding(key.WithKeys("enter", "return")),
	backspace: key.NewBinding(key.WithKeys("backspace")),
	cancel:    key.NewBinding(key.WithKeys("esc")),
}

// fileExplorerFindKeys drives the find-in-tree text-input sub-mode of the file
// explorer. These are inherent text-entry keys (not user-remappable); the
// switch only catches control keys  and lets printable characters fall
// through to the query buffer.
var fileExplorerFindKeys = struct {
	cancel    key.Binding
	escape    key.Binding
	navUp     key.Binding
	navDown   key.Binding
	enter     key.Binding
	backspace key.Binding
}{
	cancel:    key.NewBinding(key.WithKeys("ctrl+c")),
	escape:    key.NewBinding(key.WithKeys("esc")),
	navUp:     key.NewBinding(key.WithKeys("up")),
	navDown:   key.NewBinding(key.WithKeys("down")),
	enter:     key.NewBinding(key.WithKeys("enter")),
	backspace: key.NewBinding(key.WithKeys("backspace")),
}

// fileExplorerAnnotateKeys drives the inline annotation text-input sub-mode.
var fileExplorerAnnotateKeys = struct {
	cancel    key.Binding
	escape    key.Binding
	enter     key.Binding
	backspace key.Binding
}{
	cancel:    key.NewBinding(key.WithKeys("ctrl+c")),
	escape:    key.NewBinding(key.WithKeys("esc")),
	enter:     key.NewBinding(key.WithKeys("enter")),
	backspace: key.NewBinding(key.WithKeys("backspace")),
}

var inputViewKeys = struct {
	tab        key.Binding
	navUp      key.Binding
	navDown    key.Binding
	navigation []key.Binding
}{
	tab:     key.NewBinding(key.WithKeys("tab")),
	navUp:   key.NewBinding(key.WithKeys("up")),
	navDown: key.NewBinding(key.WithKeys("down")),
	navigation: []key.Binding{
		key.NewBinding(key.WithKeys("up")),
		key.NewBinding(key.WithKeys("down")),
		key.NewBinding(key.WithKeys("left")),
		key.NewBinding(key.WithKeys("right")),
		key.NewBinding(key.WithKeys("ctrl+a")),
		key.NewBinding(key.WithKeys("ctrl+e")),
		key.NewBinding(key.WithKeys("home")),
		key.NewBinding(key.WithKeys("end")),
	},
}
