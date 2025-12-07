package config

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
