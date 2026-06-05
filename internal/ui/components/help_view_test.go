package components

import (
	"strings"
	"testing"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	tea "charm.land/bubbletea/v2"

	ui "github.com/inference-gateway/cli/internal/ui"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

func newTestHelpView() *HelpViewImpl {
	fakeTheme := &uimocks.FakeTheme{}
	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)
	return NewHelpView(fakeThemeService, styles.NewProvider(fakeThemeService))
}

func sampleHelpContent() ([]HelpCommand, []ui.KeyShortcut) {
	commands := []HelpCommand{
		{Name: "help", Description: "Show available shortcuts"},
		{Name: "theme", Description: "Switch to a different theme"},
		{Name: "exit", Description: "Exit the chat session"},
	}
	keybindings := []ui.KeyShortcut{
		{Key: "ctrl+c", Description: "Cancel"},
		{Key: "esc", Description: "Close overlay"},
	}
	return commands, keybindings
}

func TestNewHelpView_Defaults(t *testing.T) {
	h := newTestHelpView()

	if h.width != 80 {
		t.Errorf("expected default width 80, got %d", h.width)
	}
	if h.IsCancelled() {
		t.Error("expected new help view to not be cancelled")
	}
}

func TestHelpView_RendersBothTables(t *testing.T) {
	h := newTestHelpView()
	commands, keybindings := sampleHelpContent()

	h.SetContent(commands, keybindings)
	h.SetWidth(100)
	h.SetHeight(100)

	out := h.View().Content

	wants := []string{
		"Commands",
		"Command",
		"Description",
		"Keybindings",
		"Key",
		"Action",
		"/help",
		"Show available shortcuts",
		"/theme",
		"ctrl+c",
		"Cancel",
		"esc",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("expected help output to contain %q\n---\n%s", w, out)
		}
	}
}

func TestHelpView_EmptyContentShowsPlaceholders(t *testing.T) {
	h := newTestHelpView()
	h.SetContent(nil, nil)
	h.SetWidth(80)
	h.SetHeight(100)

	out := h.View().Content
	if !strings.Contains(out, "No commands available") {
		t.Errorf("expected empty-commands placeholder, got:\n%s", out)
	}
	if !strings.Contains(out, "No keybindings available") {
		t.Errorf("expected empty-keybindings placeholder, got:\n%s", out)
	}
}

func TestHelpView_DismissKeysCancel(t *testing.T) {
	dismissKeys := []tea.KeyPressMsg{
		{Code: tea.KeyEscape},
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	}

	for _, key := range dismissKeys {
		h := newTestHelpView()
		h.SetContent(sampleHelpContent())

		if h.IsCancelled() {
			t.Fatal("expected fresh help view to not be cancelled")
		}

		_, _ = h.Update(key)
		if !h.IsCancelled() {
			t.Errorf("expected key %q to cancel the help view", key.String())
		}
	}
}

func TestHelpView_ScrollMovesViewport(t *testing.T) {
	h := newTestHelpView()
	// Many rows so content overflows a short viewport and scrolling is required.
	commands := make([]HelpCommand, 0, 40)
	for i := 0; i < 40; i++ {
		commands = append(commands, HelpCommand{Name: "cmd", Description: "description"})
	}
	h.SetContent(commands, nil)
	h.SetWidth(80)
	h.SetHeight(10)

	if got := h.viewport.YOffset(); got != 0 {
		t.Fatalf("expected viewport to start at top, got offset %d", got)
	}

	_, _ = h.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if h.viewport.YOffset() == 0 {
		t.Error("expected scroll down to move the viewport offset off the top")
	}

	_, _ = h.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	if got := h.viewport.YOffset(); got != 0 {
		t.Errorf("expected home to scroll back to top, got offset %d", got)
	}
}
