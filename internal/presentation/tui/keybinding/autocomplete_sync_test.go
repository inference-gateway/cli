package keybinding_test

import (
	"testing"

	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	keybinding "github.com/inference-gateway/cli/internal/presentation/tui/keybinding"
)

// staleAutocomplete still shows the suggestion for the input before its last
// keys reached it, and has none once it sees the input as typed.
func staleAutocomplete(typed string) *tuimocks.FakeAutocompleteComponent {
	autocomplete := &tuimocks.FakeAutocompleteComponent{}
	seen := ""
	autocomplete.UpdateCalls(func(text string, _ int) { seen = text })
	autocomplete.IsVisibleCalls(func() bool { return seen != typed })
	autocomplete.HandleKeyReturns(true, "/transfer ")
	return autocomplete
}

func TestEnterSubmitsWhatWasTypedEvenBeforeTheAutocompleteCaughtUp(t *testing.T) {
	const typed = "/transfer b"
	ctx := newTestContext(tui.ViewStateChat, typed)
	input := ctx.GetInputView().(*tuimocks.FakeInputComponent)
	input.GetCursorReturns(len(typed))
	autocomplete := staleAutocomplete(typed)
	ctx.GetAutocompleteReturns(autocomplete)
	ctx.SendMessageReturns(func() tea.Msg { return nil })

	keybinding.NewDispatcher(ctx, nil).ProcessKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if autocomplete.HandleKeyCallCount() != 0 {
		t.Error("enter took the stale suggestion and dropped what was typed after it")
	}
	if ctx.SendMessageCallCount() != 1 {
		t.Errorf("sends = %d, want %q submitted", ctx.SendMessageCallCount(), typed)
	}
}

func TestEnterLeavesADismissedAutocompleteDismissed(t *testing.T) {
	ctx := newTestContext(tui.ViewStateChat, "/tra")
	ctx.GetInputView().(*tuimocks.FakeInputComponent).GetCursorReturns(4)
	autocomplete := &tuimocks.FakeAutocompleteComponent{}
	ctx.GetAutocompleteReturns(autocomplete)
	ctx.SendMessageReturns(func() tea.Msg { return nil })

	keybinding.NewDispatcher(ctx, nil).ProcessKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if autocomplete.UpdateCallCount() != 0 || ctx.SendMessageCallCount() != 1 {
		t.Error("enter brought a dismissed autocomplete back instead of sending the input")
	}
}
