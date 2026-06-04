package components

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"
)

func newTestThemeSelector(t *testing.T, themes []string, current string) (*ThemeSelectorImpl, *domainmocks.FakeThemeService) {
	t.Helper()
	fakeTheme := &uimocks.FakeTheme{}
	ts := &domainmocks.FakeThemeService{}
	ts.GetCurrentThemeReturns(fakeTheme)
	ts.ListThemesReturns(themes)
	ts.GetCurrentThemeNameReturns(current)
	sp := styles.NewProvider(ts)
	return NewThemeSelector(ts, sp), ts
}

func TestThemeSelector_PreselectsCurrentTheme(t *testing.T) {
	sel, _ := newTestThemeSelector(t, []string{"a", "b", "c"}, "b")
	if got := sel.list.Index(); got != 1 {
		t.Fatalf("expected cursor on current theme index 1, got %d", got)
	}
}

func TestThemeSelector_EnterSelectsAndEmitsEvent(t *testing.T) {
	sel, ts := newTestThemeSelector(t, []string{"a", "b", "c"}, "b")

	model, cmd := sel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	sel = model.(*ThemeSelectorImpl)

	if !sel.IsSelected() || sel.IsCancelled() {
		t.Fatalf("expected selected and not cancelled, got selected=%v cancelled=%v", sel.IsSelected(), sel.IsCancelled())
	}
	if got := sel.GetSelected(); got != "b" {
		t.Fatalf("expected selected theme 'b', got %q", got)
	}
	if ts.SetThemeCallCount() != 1 {
		t.Fatalf("expected SetTheme called once, got %d", ts.SetThemeCallCount())
	}
	if cmd == nil {
		t.Fatal("expected a ThemeSelectedEvent command")
	}
	if ev, ok := cmd().(domain.ThemeSelectedEvent); !ok || ev.Theme != "b" {
		t.Fatalf("expected ThemeSelectedEvent{Theme: b}, got %#v", cmd())
	}
}

func TestThemeSelector_EscCancelsWithoutQuitting(t *testing.T) {
	sel, _ := newTestThemeSelector(t, []string{"a", "b"}, "a")

	model, cmd := sel.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	sel = model.(*ThemeSelectorImpl)

	if !sel.IsCancelled() || sel.IsSelected() {
		t.Fatalf("expected cancelled and not selected, got cancelled=%v selected=%v", sel.IsCancelled(), sel.IsSelected())
	}
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("esc must cancel the selector, not quit the program")
		}
	}
}

func TestThemeSelector_ResetRebuildsForNewCurrentTheme(t *testing.T) {
	sel, ts := newTestThemeSelector(t, []string{"a", "b", "c"}, "a")
	sel.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	ts.GetCurrentThemeNameReturns("c")
	sel.Reset()

	if sel.IsSelected() || sel.IsCancelled() {
		t.Fatalf("expected clean state after Reset, got selected=%v cancelled=%v", sel.IsSelected(), sel.IsCancelled())
	}
	if got := sel.list.Index(); got != 2 {
		t.Fatalf("expected cursor on new current theme index 2 after Reset, got %d", got)
	}
}
