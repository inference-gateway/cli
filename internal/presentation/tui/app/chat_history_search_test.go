package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
	keybinding "github.com/inference-gateway/cli/internal/presentation/tui/keybinding"
)

func newHistorySearchTestApp(t *testing.T, draft string, history ...string) (*ChatApplication, *components.InputView) {
	t.Helper()

	app, inputView := newInputRoutingTestApp(t, tui.ViewStateChat, draft)
	app.historySearchView = components.NewHistorySearchView(nil)
	app.keyBindingManager = keybinding.NewDispatcher(app, nil)

	for _, entry := range history {
		if err := inputView.AddToHistory(entry); err != nil {
			t.Fatalf("AddToHistory(%q): %v", entry, err)
		}
	}
	return app, inputView
}

func ctrlRKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
}

// openHistorySearch presses ctrl+r and feeds the resulting event back, the way
// the real Update loop does when the keybinding action emits it.
func openHistorySearch(t *testing.T, app *ChatApplication) {
	t.Helper()

	_, cmd := app.Update(ctrlRKey())
	if cmd == nil {
		t.Fatal("ctrl+r should dispatch the history search action")
	}
	msg := cmd()
	if _, ok := msg.(tui.HistorySearchOpenEvent); ok {
		_, _ = app.Update(msg)
	}
	if !app.historySearchFocused {
		t.Fatal("history search overlay should hold key focus after ctrl+r")
	}
	if !app.historySearchView.IsVisible() {
		t.Fatal("history search overlay should be visible after ctrl+r")
	}
}

func TestHistorySearchOpenDoesNotTouchInput(t *testing.T) {
	app, inputView := newHistorySearchTestApp(t, "draft prompt", "run tests", "write docs")

	openHistorySearch(t, app)

	if got := inputView.GetInput(); got != "draft prompt" {
		t.Fatalf("input after ctrl+r = %q, want the untouched draft", got)
	}
}

func TestHistorySearchTypingFiltersAndEnterLoadsPrompt(t *testing.T) {
	app, inputView := newHistorySearchTestApp(t, "", "first prompt", "second prompt", "third prompt")

	openHistorySearch(t, app)

	for _, key := range []tea.KeyPressMsg{printableKey("s"), printableKey("e"), printableKey("c")} {
		_, _ = app.Update(key)
	}
	if got := inputView.GetInput(); got != "" {
		t.Fatalf("query keys leaked into input: %q", got)
	}

	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := inputView.GetInput(); got != "second prompt" {
		t.Fatalf("input after enter = %q, want %q (fuzzy-filtered selection)", got, "second prompt")
	}
	if app.historySearchFocused || app.historySearchView.IsVisible() {
		t.Fatal("overlay should close after accepting")
	}

	assertNoUserInputEvent(t, cmd)
}

func TestHistorySearchEnterWithEmptyQueryLoadsNewestPrompt(t *testing.T) {
	app, inputView := newHistorySearchTestApp(t, "", "old prompt", "second prompt", "newest prompt")

	openHistorySearch(t, app)

	_, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := inputView.GetInput(); got != "newest prompt" {
		t.Fatalf("input after enter = %q, want %q (newest listed first)", got, "newest prompt")
	}
}

func TestHistorySearchOpenCollapsesDuplicatesToMostRecent(t *testing.T) {
	app, inputView := newHistorySearchTestApp(t, "", "first prompt", "second prompt", "duplicate prompt", "third prompt", "duplicate prompt")

	openHistorySearch(t, app)

	_, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := inputView.GetInput(); got != "duplicate prompt" {
		t.Fatalf("input after enter = %q, want %q (most recent occurrence leads)", got, "duplicate prompt")
	}
}

func TestHistorySearchEnterLoadsMultilinePrompt(t *testing.T) {
	app, inputView := newHistorySearchTestApp(t, "", "single prompt", "first line\nsecond line")

	openHistorySearch(t, app)

	_, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	_, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := inputView.GetInput(); got != "single prompt" {
		t.Fatalf("input after down+enter = %q, want %q", got, "single prompt")
	}

	openHistorySearch(t, app)

	_, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := inputView.GetInput(); got != "first line\nsecond line" {
		t.Fatalf("input after enter = %q, want the multi-line prompt restored with newlines", got)
	}
}

func TestHistorySearchEscRestoresInputAndDoesNotCountTowardsDoubleEsc(t *testing.T) {
	app, inputView := newHistorySearchTestApp(t, "draft prompt", "second prompt")

	openHistorySearch(t, app)

	_, _ = app.Update(printableKey("r"))

	_, _ = app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.historySearchFocused || app.historySearchView.IsVisible() {
		t.Fatal("esc should close the overlay")
	}
	if got := inputView.GetInput(); got != "draft prompt" {
		t.Fatalf("input after esc = %q, want the untouched draft", got)
	}

	_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("a second esc should reach the dispatcher")
	}
	if msg := cmd(); msg != nil {
		if _, ok := msg.(tui.NavigateBackInTimeEvent); ok {
			t.Fatal("overlay esc must not count towards the double-esc go-back-in-time binding")
		}
	}
}

// assertNoUserInputEvent walks a command's message tree and fails if a user
// input event (a sent prompt) is found: enter in the overlay loads, never sends.
func assertNoUserInputEvent(t *testing.T, cmd tea.Cmd) {
	t.Helper()

	if cmd == nil {
		return
	}

	switch msg := cmd().(type) {
	case agentdomain.UserInputEvent:
		t.Fatal("enter in the history search must load the prompt, not send it")
	case tea.BatchMsg:
		for _, subCmd := range msg {
			assertNoUserInputEvent(t, subCmd)
		}
	}
}
