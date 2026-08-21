package components

import (
	"testing"

	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	shortcutsmocks "github.com/inference-gateway/cli/tests/mocks/shortcuts"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
)

func newInputViewWithHighlightDeps(t *testing.T) *InputView {
	t.Helper()
	iv := createInputViewWithTheme(createMockModelService())

	fake := &agentdomainmocks.FakeSkillsService{}
	fake.GetStub = func(name string) (agentdomain.Skill, bool) {
		if name == "maintainer" {
			return agentdomain.Skill{Name: name}, true
		}
		return agentdomain.Skill{}, false
	}
	iv.SetSkillsService(fake)

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
