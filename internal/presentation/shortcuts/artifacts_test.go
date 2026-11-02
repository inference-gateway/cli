package shortcuts

import "testing"

func TestArtifactsShortcutOpensTheView(t *testing.T) {
	shortcut := NewArtifactsShortcut()

	result, err := shortcut.Execute(t.Context(), nil)

	if err != nil || result.SideEffect != SideEffectShowArtifacts {
		t.Fatalf("Execute = %+v, %v, want the artifacts view", result, err)
	}
}
