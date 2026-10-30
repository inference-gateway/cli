package shortcuts

// NewA2ATaskManagementShortcut opens the task view: every background job and its
// history, whether or not A2A is enabled.
func NewA2ATaskManagementShortcut() *panelShortcut {
	return newPanelShortcut("tasks", "Show background tasks and their history", SideEffectShowA2ATaskManagement)
}
