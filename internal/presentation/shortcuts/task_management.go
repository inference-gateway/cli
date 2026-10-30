package shortcuts

// NewA2ATaskManagementShortcut opens the task view: every background job and its
// history, whether or not A2A is enabled.
func NewA2ATaskManagementShortcut() *PanelShortcut {
	return &PanelShortcut{name: "tasks", description: "Show background tasks and their history", sideEffect: SideEffectShowA2ATaskManagement}
}
