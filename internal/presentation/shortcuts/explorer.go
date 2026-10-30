package shortcuts

// NewExplorerShortcut opens the file explorer panel (VS Code-style tree + fuzzy
// finder). It is a pure view trigger with no dependencies - the side effect
// transitions the UI to the explorer, which walks the working directory itself.
func NewExplorerShortcut() *panelShortcut {
	return newPanelShortcut("explorer", "Open the file explorer (tree + fuzzy finder)", SideEffectShowExplorer)
}
