package shortcuts

// NewConversationSelectShortcut shows the conversation selection dropdown.
func NewConversationSelectShortcut() *panelShortcut {
	return newPanelShortcut("conversations", "Show conversation selection dropdown", SideEffectShowConversationSelection)
}
