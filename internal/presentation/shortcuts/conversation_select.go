package shortcuts

// NewConversationSelectShortcut shows the conversation selection dropdown.
func NewConversationSelectShortcut() *PanelShortcut {
	return &PanelShortcut{name: "conversations", description: "Show conversation selection dropdown", sideEffect: SideEffectShowConversationSelection}
}
