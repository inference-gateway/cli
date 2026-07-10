package components

import (
	"context"
	"testing"

	require "github.com/stretchr/testify/require"

	domain "github.com/inference-gateway/cli/internal/domain"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	shortcutsmocks "github.com/inference-gateway/cli/tests/mocks/shortcuts"
)

// stubSkillsService is a minimal domain.SkillsService for highlight tests; there
// is no counterfeiter fake for this interface.
type stubSkillsService struct{ known map[string]bool }

func (s *stubSkillsService) Load(context.Context) error { return nil }
func (s *stubSkillsService) List() []domain.Skill       { return nil }
func (s *stubSkillsService) Get(name string) (domain.Skill, bool) {
	if s.known[name] {
		return domain.Skill{Name: name}, true
	}
	return domain.Skill{}, false
}
func (s *stubSkillsService) Errors() []domain.SkillLoadError { return nil }

func newInputViewWithHighlightDeps(t *testing.T) *InputView {
	t.Helper()
	iv := createInputViewWithTheme(createMockModelService())
	iv.SetSkillsService(&stubSkillsService{known: map[string]bool{"maintainer": true}})

	registry := shortcuts.NewRegistry()
	gitShortcut := &shortcutsmocks.FakeShortcut{}
	gitShortcut.GetNameReturns("git")
	registry.Register(gitShortcut)
	iv.SetShortcutRegistry(registry)

	return iv
}

func TestInputView_Highlighter_LazyBuiltAndStable(t *testing.T) {
	iv := newInputViewWithHighlightDeps(t)

	require.Nil(t, iv.highlighter, "highlighter should not exist before first render")

	iv.SetText("/maintainer")
	iv.SetCursor(len(iv.GetInput()))
	require.NotEmpty(t, iv.Render())
	require.NotNil(t, iv.highlighter, "highlighter should be lazily built on render")

	built := iv.highlighter
	require.NotEmpty(t, iv.Render())
	require.Same(t, built, iv.highlighter, "highlighter should be built once and reused")
}

func TestInputView_Highlighter_NotBuiltWithoutServices(t *testing.T) {
	iv := createInputViewWithTheme(createMockModelService())

	iv.SetText("/maintainer")
	iv.SetCursor(len(iv.GetInput()))
	require.NotEmpty(t, iv.Render(), "render must not panic without highlight services")
	require.Nil(t, iv.highlighter, "no rules wired => no highlighter")
}

func TestInputView_Highlighter_SkippedInBashAndToolsModes(t *testing.T) {
	iv := newInputViewWithHighlightDeps(t)

	iv.SetText("!ls /maintainer")
	iv.SetCursor(len(iv.GetInput()))
	require.NotEmpty(t, iv.Render())
	require.Nil(t, iv.highlighter, "bash mode must skip the highlighter entirely")

	iv.SetText("!!Grep(/maintainer)")
	iv.SetCursor(len(iv.GetInput()))
	require.NotEmpty(t, iv.Render())
	require.Nil(t, iv.highlighter, "tools mode must skip the highlighter entirely")

	// Switching back to a normal prompt builds and applies it.
	iv.SetText("/maintainer")
	iv.SetCursor(len(iv.GetInput()))
	require.NotEmpty(t, iv.Render())
	require.NotNil(t, iv.highlighter, "normal mode should build the highlighter")
}

func TestInputView_Highlighter_RendersTokensWithoutPanic(t *testing.T) {
	iv := newInputViewWithHighlightDeps(t)

	for _, text := range []string{
		"/maintainer fix the bug",
		"use /maintainer please",
		"/git status",
		"path/maintainer is not a token",
		"/unknown stays plain",
	} {
		iv.SetText(text)
		iv.SetCursor(len(iv.GetInput()))
		require.NotEmpty(t, iv.Render(), "render should succeed for %q", text)
	}
}
