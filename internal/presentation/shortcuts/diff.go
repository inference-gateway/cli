package shortcuts

// NewDiffShortcut opens the changes panel (interactive diff viewer). It is a pure
// view trigger with no dependencies - the side effect transitions the UI to the
// diff viewer, which loads working-tree changes itself. The viewer has two tabs
// (Local working-tree changes and the current branch's PR diff), switched with
// Tab; the PR tab is read-only.
func NewDiffShortcut() *panelShortcut {
	return newPanelShortcut("diff", "Open the changes panel (interactive diff viewer)", SideEffectShowDiffViewer)
}
