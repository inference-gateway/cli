package config

import (
	utils "github.com/inference-gateway/cli/config/utils"
)

const (
	KeybindingsFileName    = "keybindings.yaml"
	DefaultKeybindingsPath = ConfigDirName + "/" + KeybindingsFileName
)

// KeybindingsConfig contains settings for customizing keybindings
type KeybindingsConfig struct {
	Enabled  bool                       `yaml:"enabled" mapstructure:"enabled"`
	Bindings map[string]KeyBindingEntry `yaml:"bindings,omitempty" mapstructure:"bindings,omitempty"`
}

// KeyBindingEntry defines a complete keybinding with its properties
type KeyBindingEntry struct {
	Keys        []string `yaml:"keys" mapstructure:"keys"`
	Description string   `yaml:"description,omitempty" mapstructure:"description,omitempty"`
	Category    string   `yaml:"category,omitempty" mapstructure:"category,omitempty"`
	Enabled     *bool    `yaml:"enabled,omitempty" mapstructure:"enabled,omitempty"`
}

// DefaultKeybindingsConfig returns the default keybindings config used when
// no file exists. Callers (init, reset) use it to seed a fresh file.
func DefaultKeybindingsConfig() *KeybindingsConfig {
	return &KeybindingsConfig{
		Enabled:  true,
		Bindings: GetDefaultKeybindings(),
	}
}

// LoadKeybindings reads keybindings.yaml from disk. When the file is
// missing it returns the in-code defaults so callers can treat absence
// as "use defaults" without special-casing. The file body is run through
// os.ExpandEnv - any literal `${…}` token in a customised binding must be
// escaped as `$$…`.
func LoadKeybindings(path string) (*KeybindingsConfig, error) {
	return utils.LoadYAML(path, "keybindings", DefaultKeybindingsConfig)
}

// SaveKeybindings writes the keybindings configuration to disk, creating
// any missing parent directories.
func SaveKeybindings(path string, cfg *KeybindingsConfig) error {
	return utils.SaveYAML(path, "keybindings", cfg)
}

// GetDefaultKeybindings returns the default keybinding configuration
// Users can override these in their config file, and any missing entries
// will fall back to these defaults
func GetDefaultKeybindings() map[string]KeyBindingEntry {
	bindings := make(map[string]KeyBindingEntry)

	addGlobalBindings(bindings)
	addChatBindings(bindings)
	addDisplayBindings(bindings)
	addNavigationBindings(bindings)
	addTextEditingBindings(bindings)
	addClipboardBindings(bindings)
	addPlanApprovalBindings(bindings)
	addModeBindings(bindings)
	addToolsBindings(bindings)
	addSelectionBindings(bindings)
	addHelpBindings(bindings)
	addDiffViewerBindings(bindings)

	return bindings
}

func addGlobalBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceGlobal, "quit")] = KeyBindingEntry{
		Keys:        []string{"ctrl+c"},
		Description: "exit application",
		Category:    "global",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceGlobal, "cancel")] = KeyBindingEntry{
		Keys:        []string{"esc"},
		Description: "cancel current operation",
		Category:    "global",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceGlobal, "new_session")] = KeyBindingEntry{
		Keys:        []string{"ctrl+l"},
		Description: "clear chat and start a new session",
		Category:    "global",
		Enabled:     &enabled,
	}
}

func addChatBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceChat, "enter_key_handler")] = KeyBindingEntry{
		Keys:        []string{"enter"},
		Description: "send message or insert newline",
		Category:    "chat",
		Enabled:     &enabled,
	}
}

func addDisplayBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceDisplay, "toggle_raw_format")] = KeyBindingEntry{
		Keys:        []string{"ctrl+r"},
		Description: "toggle raw/rendered markdown",
		Category:    "display",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceDisplay, "toggle_todo_box")] = KeyBindingEntry{
		Keys:        []string{"ctrl+t"},
		Description: "toggle todo list",
		Category:    "display",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceDisplay, "toggle_thinking")] = KeyBindingEntry{
		Keys:        []string{"ctrl+k"},
		Description: "expand/collapse thinking blocks",
		Category:    "display",
		Enabled:     &enabled,
	}
}

func addNavigationBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceNavigation, "scroll_to_top")] = KeyBindingEntry{
		Keys:        []string{"home"},
		Description: "scroll to top",
		Category:    "navigation",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceNavigation, "scroll_to_bottom")] = KeyBindingEntry{
		Keys:        []string{"end"},
		Description: "scroll to bottom",
		Category:    "navigation",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceNavigation, "scroll_up_half_page")] = KeyBindingEntry{
		Keys:        []string{"shift+up"},
		Description: "scroll up half page",
		Category:    "navigation",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceNavigation, "scroll_down_half_page")] = KeyBindingEntry{
		Keys:        []string{"shift+down"},
		Description: "scroll down half page",
		Category:    "navigation",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceNavigation, "page_up")] = KeyBindingEntry{
		Keys:        []string{"pgup", "page_up"},
		Description: "page up",
		Category:    "navigation",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceNavigation, "page_down")] = KeyBindingEntry{
		Keys:        []string{"pgdn", "page_down"},
		Description: "page down",
		Category:    "navigation",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceNavigation, "go_back_in_time")] = KeyBindingEntry{
		Keys:        []string{"esc,esc"},
		Description: "go back in time to previous message (double ESC)",
		Category:    "navigation",
		Enabled:     &enabled,
	}
}

func addTextEditingBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceTextEditing, "insert_newline_alt")] = KeyBindingEntry{
		Keys:        []string{"alt+enter"},
		Description: "insert newline",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "insert_newline_ctrl")] = KeyBindingEntry{
		Keys:        []string{"ctrl+j"},
		Description: "insert newline",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "move_cursor_left")] = KeyBindingEntry{
		Keys:        []string{"left"},
		Description: "move cursor left",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "move_cursor_right")] = KeyBindingEntry{
		Keys:        []string{"right"},
		Description: "move cursor right",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "backspace")] = KeyBindingEntry{
		Keys:        []string{"backspace"},
		Description: "delete character",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "delete_to_beginning")] = KeyBindingEntry{
		Keys:        []string{"ctrl+u"},
		Description: "delete to beginning of line",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "delete_word_backward")] = KeyBindingEntry{
		Keys:        []string{"ctrl+w"},
		Description: "delete word backward",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "delete_word_forward")] = KeyBindingEntry{
		Keys:        []string{"alt+d"},
		Description: "delete word forward",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "move_cursor_word_left")] = KeyBindingEntry{
		Keys:        []string{"alt+left", "ctrl+left", "alt+b"},
		Description: "move cursor one word left",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "move_cursor_word_right")] = KeyBindingEntry{
		Keys:        []string{"alt+right", "ctrl+right", "alt+f"},
		Description: "move cursor one word right",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "move_to_beginning")] = KeyBindingEntry{
		Keys:        []string{"ctrl+a"},
		Description: "move cursor to beginning",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "move_to_end")] = KeyBindingEntry{
		Keys:        []string{"ctrl+e"},
		Description: "move cursor to end",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "history_up")] = KeyBindingEntry{
		Keys:        []string{"up"},
		Description: "navigate to previous message in history",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTextEditing, "history_down")] = KeyBindingEntry{
		Keys:        []string{"down"},
		Description: "navigate to next message in history",
		Category:    "text_editing",
		Enabled:     &enabled,
	}
}

func addClipboardBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceClipboard, "paste_text")] = KeyBindingEntry{
		Keys:        []string{"ctrl+v"},
		Description: "paste text",
		Category:    "clipboard",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceClipboard, "copy_text")] = KeyBindingEntry{
		Keys:        []string{"ctrl+shift+c"},
		Description: "copy text",
		Category:    "clipboard",
		Enabled:     &enabled,
	}
}

func addPlanApprovalBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespacePlanApproval, "plan_approval_left")] = KeyBindingEntry{
		Keys:        []string{"left", "h"},
		Description: "move selection left",
		Category:    "plan_approval",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespacePlanApproval, "plan_approval_right")] = KeyBindingEntry{
		Keys:        []string{"right", "l"},
		Description: "move selection right",
		Category:    "plan_approval",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespacePlanApproval, "plan_approval_accept")] = KeyBindingEntry{
		Keys:        []string{"enter", "y"},
		Description: "accept plan",
		Category:    "plan_approval",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespacePlanApproval, "plan_approval_reject")] = KeyBindingEntry{
		Keys:        []string{"n"},
		Description: "reject plan",
		Category:    "plan_approval",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespacePlanApproval, "plan_approval_accept_and_auto_approve")] = KeyBindingEntry{
		Keys:        []string{"a"},
		Description: "accept plan and enable auto-approve mode",
		Category:    "plan_approval",
		Enabled:     &enabled,
	}
}

func addModeBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceMode, "cycle_agent_mode")] = KeyBindingEntry{
		Keys:        []string{"shift+tab"},
		Description: "cycle agent mode (Standard/Plan/Auto-Accept)",
		Category:    "mode",
		Enabled:     &enabled,
	}
}

func addToolsBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceTools, "toggle_tool_expansion")] = KeyBindingEntry{
		Keys:        []string{"ctrl+o"},
		Description: "expand/collapse tool results",
		Category:    "tools",
		Enabled:     &enabled,
	}
	bindings[ActionID(NamespaceTools, "background_shell")] = KeyBindingEntry{
		Keys:        []string{"ctrl+b"},
		Description: "move running bash command to background",
		Category:    "tools",
		Enabled:     &enabled,
	}
}

func addSelectionBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceSelection, "toggle_mouse_mode")] = KeyBindingEntry{
		Keys:        []string{"ctrl+s"},
		Description: "toggle mouse scrolling/text selection",
		Category:    "selection",
		Enabled:     &enabled,
	}
}

func addHelpBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	bindings[ActionID(NamespaceHelp, "toggle_help")] = KeyBindingEntry{
		Keys:        []string{"?"},
		Description: "toggle help when input is empty",
		Category:    "help",
		Enabled:     &enabled,
	}
}

// addDiffViewerBindings registers the configurable keys for the `/diff` changes
// panel (the self-contained overlay opened with /diff). Both the file-tree and
// the hunk (patch) modes resolve their keys from these entries.
func addDiffViewerBindings(bindings map[string]KeyBindingEntry) {
	enabled := true
	add := func(action, desc string, keys ...string) {
		bindings[ActionID(NamespaceDiffViewer, action)] = KeyBindingEntry{
			Keys:        keys,
			Description: desc,
			Category:    string(NamespaceDiffViewer),
			Enabled:     &enabled,
		}
	}
	add("nav_up", "move selection up", "up", "k")
	add("nav_down", "move selection down", "down", "j")
	add("collapse", "collapse section/folder", "left", "h")
	add("expand", "expand section/folder", "right", "l")
	add("toggle", "toggle section/folder collapse", "enter", "space")
	add("stage", "stage the selected file", "a")
	add("unstage", "unstage the selected file", "u")
	add("discard", "discard a file's working-tree changes", "d")
	add("patch", "stage hunks (enter patch mode)", "p")
	add("edit", "edit the selected file (vim mode)", "v")
	add("commit", "commit staged changes", "c")
	add("scroll_up", "scroll the diff up", "pgup")
	add("scroll_down", "scroll the diff down", "pgdown", "pgdn")
	add("halfpage_up", "scroll the diff up half a page", "ctrl+u")
	add("halfpage_down", "scroll the diff down half a page", "ctrl+d")
	add("patch_apply", "apply the current hunk", "a", "s", "u", "enter", "space")
	add("cancel", "close the changes panel / exit patch mode", "esc", "q")
}

// ResolveNamespaceBindings returns the effective key lists for every default
// action in the namespace, applying any user overrides from cfg (and skipping
// actions the user explicitly disabled). Action IDs absent from cfg fall back
// to their built-in default keys, so the result is complete even for
// keybindings.yaml files written before the namespace existed.
func ResolveNamespaceBindings(cfg KeybindingsConfig, ns KeyNamespace) map[string][]string {
	out := make(map[string][]string)
	for id, def := range GetDefaultKeybindings() {
		if def.Category != string(ns) {
			continue
		}
		keys := def.Keys
		if ov, ok := cfg.Bindings[id]; ok {
			if ov.Enabled != nil && !*ov.Enabled {
				continue
			}
			if len(ov.Keys) > 0 {
				keys = ov.Keys
			}
		}
		out[id] = keys
	}
	return out
}
