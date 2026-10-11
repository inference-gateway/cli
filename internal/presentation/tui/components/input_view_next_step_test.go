package components

import (
	"testing"

	require "github.com/stretchr/testify/require"

	tea "charm.land/bubbletea/v2"
)

func TestInputView_NextStepSuggestion(t *testing.T) {
	iv := createInputViewWithTheme(createMockModelService())

	require.False(t, iv.AcceptNextStepSuggestion())

	iv.SetNextStepSuggestion("run the failing test")
	require.True(t, iv.HasNextStepSuggestion())

	require.True(t, iv.AcceptNextStepSuggestion())
	require.Equal(t, "run the failing test", iv.GetInput())
	require.Equal(t, len(iv.GetInput()), iv.GetCursor())
	require.False(t, iv.HasNextStepSuggestion())

	iv.SetNextStepSuggestion("")
	require.False(t, iv.HasNextStepSuggestion())
}

func TestInputView_NextStepSuggestionDismissal(t *testing.T) {
	iv := createInputViewWithTheme(createMockModelService())

	iv.SetNextStepSuggestion("run the failing test")
	iv.focused = true
	_ = iv.ta.Focus()
	_, _ = iv.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	require.False(t, iv.HasNextStepSuggestion())

	iv.SetNextStepSuggestion("run the failing test")
	iv.ClearInput()
	require.False(t, iv.HasNextStepSuggestion())
}
