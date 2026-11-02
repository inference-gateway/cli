package shortcuts

// NewArtifactsShortcut opens the view of the session's artifacts on the
// workspace it runs on, which saves one here.
func NewArtifactsShortcut() *PanelShortcut {
	return &PanelShortcut{name: "artifacts", description: "Browse this session's artifacts and save one here", sideEffect: SideEffectShowArtifacts}
}
