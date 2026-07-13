package components

import (
	"strings"
	"testing"

	key "charm.land/bubbles/v2/key"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// kb builds a key.Binding with a help key/description for help-bar tests.
func kb(k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc))
}

// createMockStyleProviderForHelpBar creates a mock styles provider for testing
func createMockStyleProviderForHelpBar() *styles.Provider {
	fakeTheme := &uimocks.FakeTheme{}
	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)
	return styles.NewProvider(fakeThemeService)
}

func TestNewHelpBar(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())

	if hb.width != 80 {
		t.Errorf("Expected default width 80, got %d", hb.width)
	}

	if hb.IsEnabled() {
		t.Error("Expected help bar to be disabled by default")
	}

	if len(hb.shortcuts) != 0 {
		t.Errorf("Expected empty shortcuts initially, got %d", len(hb.shortcuts))
	}
}

func TestHelpBar_SetShortcuts(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())

	shortcuts := []key.Binding{
		kb("Enter", "Send message"),
		kb("Ctrl+C", "Cancel"),
		kb("↑↓", "History"),
	}

	hb.SetShortcuts(shortcuts)

	if len(hb.shortcuts) != 3 {
		t.Errorf("Expected 3 shortcuts, got %d", len(hb.shortcuts))
	}

	if got := hb.shortcuts[0].Help().Key; got != "Ctrl+C" {
		t.Errorf("Expected first shortcut key 'Ctrl+C', got '%s'", got)
	}

	if got := hb.shortcuts[0].Help().Desc; got != "Cancel" {
		t.Errorf("Expected first shortcut description 'Cancel', got '%s'", got)
	}

	if got := hb.shortcuts[1].Help().Key; got != "Enter" {
		t.Errorf("Expected second shortcut key 'Enter', got '%s'", got)
	}
}

func TestHelpBar_IsEnabled(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())

	if hb.IsEnabled() {
		t.Error("Expected help bar to be disabled by default")
	}
}

func TestHelpBar_SetEnabled(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())

	hb.SetEnabled(false)
	if hb.IsEnabled() {
		t.Error("Expected help bar to be disabled after SetEnabled(false)")
	}

	hb.SetEnabled(true)
	if !hb.IsEnabled() {
		t.Error("Expected help bar to be enabled after SetEnabled(true)")
	}
}

func TestHelpBar_SetWidth(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())

	hb.SetWidth(120)

	if hb.width != 120 {
		t.Errorf("Expected width 120, got %d", hb.width)
	}
}

func TestHelpBar_SetHeight(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())

	hb.SetHeight(2)
}

func TestHelpBar_Render(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		shortcuts []key.Binding
		width     int
		wantEmpty bool
		wantKeys  []string
	}{
		{
			name:      "disabled returns empty",
			enabled:   false,
			wantEmpty: true,
		},
		{
			name:      "no shortcuts returns empty",
			enabled:   true,
			wantEmpty: true,
		},
		{
			name:    "with shortcuts renders keys and descriptions",
			enabled: true,
			shortcuts: []key.Binding{
				kb("Enter", "Send"),
				kb("Ctrl+C", "Cancel"),
				kb("?", "Help"),
			},
			wantKeys: []string{"Enter", "Send", "Cancel"},
		},
		{
			name:    "long shortcuts truncated",
			enabled: true,
			width:   20,
			shortcuts: []key.Binding{
				kb("Ctrl+Shift+Alt+D", "Very long description that should be truncated"),
				kb("F1", "Short"),
			},
			wantEmpty: false,
		},
		{
			name:      "empty shortcuts array returns empty",
			enabled:   true,
			shortcuts: []key.Binding{},
			wantEmpty: true,
		},
		{
			name:    "single shortcut renders",
			enabled: true,
			shortcuts: []key.Binding{
				kb("?", "Help"),
			},
			wantKeys: []string{"?", "Help"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hb := NewHelpBar(createMockStyleProviderForHelpBar())
			hb.SetEnabled(tt.enabled)
			if tt.width > 0 {
				hb.SetWidth(tt.width)
			}
			if tt.shortcuts != nil {
				hb.SetShortcuts(tt.shortcuts)
			}
			output := hb.Render()

			if tt.wantEmpty {
				if output != "" {
					t.Errorf("expected empty output, got %q", output)
				}
				return
			}

			if output == "" {
				t.Error("expected non-empty output")
			}
			for _, k := range tt.wantKeys {
				if !strings.Contains(output, k) {
					t.Errorf("expected output to contain %q, got %q", k, output)
				}
			}
		})
	}
}
