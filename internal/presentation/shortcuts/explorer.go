package shortcuts

// NewExplorerShortcut opens the file explorer panel (VS Code-style tree + fuzzy
// finder). It is a pure view trigger with no dependencies - the side effect
// transitions the UI to the explorer, which walks the working directory itself.
func NewExplorerShortcut() *PanelShortcut {
	return &PanelShortcut{name: "explorer", description: "Open the file explorer (tree + fuzzy finder)", sideEffect: SideEffectShowExplorer}
}
