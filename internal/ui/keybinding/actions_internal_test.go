package keybinding

import (
	"testing"
	"time"

	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	tea "charm.land/bubbletea/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
	ui "github.com/inference-gateway/cli/internal/ui"
)

// flashTestCtx is a minimal KeyHandlerContext for exercising the clipboard
// feedback helpers directly. It embeds the interface (so it satisfies the full
// contract) and overrides only the accessors the helpers actually use. A
// hand-rolled fake is used here instead of the generated FakeKeyHandlerContext
// because that fake imports this package, which would create an import cycle in
// an internal (package keybinding) test.
//
// handleCopy and handlePaste are intentionally not unit-tested here: they call
// the package-level clipboard, which compiles to the real macOS pasteboard when
// tests run without the `test` build tag (the repo's `task test` does). The
// spinner-safe flash behaviour they rely on is covered via flashStatus below.
type flashTestCtx struct {
	KeyHandlerContext
	status ui.StatusComponent
	input  ui.InputComponent
}

func (c *flashTestCtx) GetStatusView() ui.StatusComponent { return c.status }
func (c *flashTestCtx) GetInputView() ui.InputComponent   { return c.input }

func newFlashCtx(spinnerActive bool, input string) *flashTestCtx {
	status := &uimocks.FakeStatusComponent{}
	status.IsShowingSpinnerReturns(spinnerActive)

	in := &uimocks.FakeInputComponent{}
	in.GetInputReturns(input)
	in.GetCursorReturns(0)

	return &flashTestCtx{status: status, input: in}
}

// TestFlashStatusPreservesActiveSpinner verifies that, while a spinner is
// running, the flash saves the status state and schedules a restore so the
// loading indicator is not interrupted (the double-esc behaviour).
func TestFlashStatusPreservesActiveSpinner(t *testing.T) {
	ctx := newFlashCtx(true, "")

	batch, ok := flashStatus(ctx, "Copied to clipboard")().(tea.BatchMsg)
	if !ok {
		t.Fatal("expected flashStatus to return a tea.BatchMsg")
	}
	if len(batch) != 3 {
		t.Fatalf("expected 3 batched commands (save, show, restore), got %d", len(batch))
	}

	if _, ok := batch[0]().(domain.SaveStatusStateEvent); !ok {
		t.Errorf("expected the first command to save the status state, got %T", batch[0]())
	}

	ev, ok := batch[1]().(domain.SetStatusEvent)
	if !ok {
		t.Fatalf("expected the second command to be a SetStatusEvent, got %T", batch[1]())
	}
	if ev.Message != "Copied to clipboard" {
		t.Errorf("expected message %q, got %q", "Copied to clipboard", ev.Message)
	}
	if ev.Spinner {
		t.Error("a flash message must not turn the spinner on")
	}
}

// TestFlashStatusClearsWhenIdle verifies that, with no spinner, the flash shows
// the message and schedules a clear-to-empty so it auto-dismisses.
func TestFlashStatusClearsWhenIdle(t *testing.T) {
	ctx := newFlashCtx(false, "")

	batch, ok := flashStatus(ctx, "Text pasted from clipboard")().(tea.BatchMsg)
	if !ok {
		t.Fatal("expected flashStatus to return a tea.BatchMsg")
	}
	if len(batch) != 2 {
		t.Fatalf("expected 2 batched commands (show, clear), got %d", len(batch))
	}

	ev, ok := batch[0]().(domain.SetStatusEvent)
	if !ok {
		t.Fatalf("expected the first command to be a SetStatusEvent, got %T", batch[0]())
	}
	if ev.Message != "Text pasted from clipboard" {
		t.Errorf("expected message %q, got %q", "Text pasted from clipboard", ev.Message)
	}
	// batch[1] clears the status line after clipboardFlashDuration; not executed.
}

// TestHandlePasteEventFlashesAndInserts verifies the bracketed-paste (Cmd+V)
// path inserts the text and flashes a "Text pasted from clipboard" confirmation.
func TestHandlePasteEventFlashesAndInserts(t *testing.T) {
	ctx := newFlashCtx(false, "")
	input := ctx.input.(*uimocks.FakeInputComponent)

	cmd := handlePasteEvent(ctx, "hello world")
	if cmd == nil {
		t.Fatal("expected a command for a non-empty paste")
	}

	if input.SetTextCallCount() != 1 {
		t.Fatalf("expected SetText to be called once, got %d", input.SetTextCallCount())
	}
	if got := input.SetTextArgsForCall(0); got != "hello world" {
		t.Errorf("expected inserted text %q, got %q", "hello world", got)
	}

	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected a tea.BatchMsg, got %T", cmd())
	}
	ev, ok := batch[0]().(domain.SetStatusEvent)
	if !ok {
		t.Fatalf("expected a SetStatusEvent, got %T", batch[0]())
	}
	if ev.Message != "Text pasted from clipboard" {
		t.Errorf("expected message %q, got %q", "Text pasted from clipboard", ev.Message)
	}
}

// TestHandlePasteEventEmptyIsNoop verifies an empty paste neither inserts text
// nor flashes a confirmation.
func TestHandlePasteEventEmptyIsNoop(t *testing.T) {
	ctx := newFlashCtx(false, "")
	input := ctx.input.(*uimocks.FakeInputComponent)

	if cmd := handlePasteEvent(ctx, "[]"); cmd != nil {
		t.Error("expected a nil command for an empty paste")
	}
	if input.SetTextCallCount() != 0 {
		t.Errorf("expected no text insertion for an empty paste, got %d calls", input.SetTextCallCount())
	}
}

// TestClipboardFlashDurationIsAFlash guards the user's constraint that the
// confirmation is a brief flash that disappears within 3 seconds.
func TestClipboardFlashDurationIsAFlash(t *testing.T) {
	if clipboardFlashDuration <= 0 || clipboardFlashDuration > 3*time.Second {
		t.Errorf("clipboardFlashDuration must be a brief flash within 3s, got %s", clipboardFlashDuration)
	}
}

// historyNavTestCtx is a minimal KeyHandlerContext for the arrow-down
// history/status-bar handoff, overriding only the accessors handleHistoryDown
// uses (hand-rolled for the same import-cycle reason as flashTestCtx).
type historyNavTestCtx struct {
	KeyHandlerContext
	input        ui.InputComponent
	autocomplete ui.AutocompleteComponent
}

func (c *historyNavTestCtx) GetInputView() ui.InputComponent           { return c.input }
func (c *historyNavTestCtx) GetAutocomplete() ui.AutocompleteComponent { return c.autocomplete }

// TestHandleHistoryDownNavigatesWhileInHistory verifies arrow-down keeps
// walking the input history while navigation is active.
func TestHandleHistoryDownNavigatesWhileInHistory(t *testing.T) {
	input := &uimocks.FakeInputComponent{}
	input.IsNavigatingHistoryReturns(true)
	ctx := &historyNavTestCtx{input: input}

	if cmd := handleHistoryDown(ctx, tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatal("expected no command while navigating history")
	}
	if input.NavigateHistoryDownCallCount() != 1 {
		t.Errorf("expected one NavigateHistoryDown call, got %d", input.NavigateHistoryDownCallCount())
	}
}

// TestHandleHistoryDownFocusesStatusBarWhenIdle verifies arrow-down hands
// focus to the status-indicator row exactly when it would otherwise no-op.
func TestHandleHistoryDownFocusesStatusBarWhenIdle(t *testing.T) {
	input := &uimocks.FakeInputComponent{}
	input.IsNavigatingHistoryReturns(false)
	ctx := &historyNavTestCtx{input: input}

	cmd := handleHistoryDown(ctx, tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd == nil {
		t.Fatal("expected a command when history navigation is idle")
	}
	if _, ok := cmd().(domain.FocusStatusBarEvent); !ok {
		t.Fatalf("expected a FocusStatusBarEvent, got %T", cmd())
	}
	if input.NavigateHistoryDownCallCount() != 0 {
		t.Errorf("history must not move when handing focus to the status bar, got %d calls", input.NavigateHistoryDownCallCount())
	}
}

// TestHandleHistoryDownPrefersAutocomplete verifies a visible autocomplete
// dropdown keeps owning arrow-down.
func TestHandleHistoryDownPrefersAutocomplete(t *testing.T) {
	input := &uimocks.FakeInputComponent{}
	autocomplete := &uimocks.FakeAutocompleteComponent{}
	autocomplete.IsVisibleReturns(true)
	ctx := &historyNavTestCtx{input: input, autocomplete: autocomplete}

	if cmd := handleHistoryDown(ctx, tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatal("expected no command while autocomplete is visible")
	}
	if autocomplete.HandleKeyCallCount() != 1 {
		t.Errorf("expected the key routed to autocomplete, got %d calls", autocomplete.HandleKeyCallCount())
	}
	if input.IsNavigatingHistoryCallCount() != 0 || input.NavigateHistoryDownCallCount() != 0 {
		t.Error("history navigation must not be touched while autocomplete is visible")
	}
}
