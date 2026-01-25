package components

import (
	"strings"
	"testing"

	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"

	ui "github.com/inference-gateway/cli/internal/ui"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

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

	shortcuts := []ui.KeyShortcut{
		{Key: "Enter", Description: "Send message"},
		{Key: "Ctrl+C", Description: "Cancel"},
		{Key: "↑↓", Description: "History"},
	}

	hb.SetShortcuts(shortcuts)

	if len(hb.shortcuts) != 3 {
		t.Errorf("Expected 3 shortcuts, got %d", len(hb.shortcuts))
	}

	if hb.shortcuts[0].Key != "Ctrl+C" {
		t.Errorf("Expected first shortcut key 'Ctrl+C', got '%s'", hb.shortcuts[0].Key)
	}

	if hb.shortcuts[0].Description != "Cancel" {
		t.Errorf("Expected first shortcut description 'Cancel', got '%s'", hb.shortcuts[0].Description)
	}

	if hb.shortcuts[1].Key != "Enter" {
		t.Errorf("Expected second shortcut key 'Enter', got '%s'", hb.shortcuts[1].Key)
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

func TestHelpBar_Render_Disabled(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())
	hb.SetEnabled(false)

	output := hb.Render()

	if output != "" {
		t.Errorf("Expected empty output when disabled, got '%s'", output)
	}
}

func TestHelpBar_Render_NoShortcuts(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())
	hb.SetEnabled(true)

	output := hb.Render()

	if output != "" {
		t.Error("Expected empty output with no shortcuts")
	}
}

func TestHelpBar_Render_WithShortcuts(t *testing.T) {
	fakeTheme := &uimocks.FakeTheme{}
	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)
	styleProvider := styles.NewProvider(fakeThemeService)
	hb := NewHelpBar(styleProvider)
	hb.SetEnabled(true)

	shortcuts := []ui.KeyShortcut{
		{Key: "Enter", Description: "Send"},
		{Key: "Ctrl+C", Description: "Cancel"},
		{Key: "?", Description: "Help"},
	}

	hb.SetShortcuts(shortcuts)
	output := hb.Render()

	if output == "" {
		t.Error("Expected non-empty output with shortcuts")
	}

	if !strings.Contains(output, "Enter") {
		t.Error("Expected output to contain 'Enter'")
	}

	if !strings.Contains(output, "Send") {
		t.Error("Expected output to contain 'Send'")
	}

	if !strings.Contains(output, "Cancel") {
		t.Error("Expected output to contain 'Cancel'")
	}
}

func TestHelpBar_Render_LongShortcuts(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())
	hb.SetEnabled(true)
	hb.SetWidth(20)

	shortcuts := []ui.KeyShortcut{
		{Key: "Ctrl+Shift+Alt+D", Description: "Very long description that should be truncated"},
		{Key: "F1", Description: "Short"},
	}

	hb.SetShortcuts(shortcuts)
	output := hb.Render()

	if output == "" {
		t.Error("Expected non-empty output with long shortcuts")
	}

	if len(output) == 0 {
		t.Error("Expected some output even with long shortcuts")
	}
}

func TestHelpBar_Render_EmptyShortcuts(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())
	hb.SetEnabled(true)

	hb.SetShortcuts([]ui.KeyShortcut{})
	output := hb.Render()

	if output != "" {
		t.Error("Expected empty output with empty shortcuts array")
	}
}

func TestHelpBar_Render_SingleShortcut(t *testing.T) {
	hb := NewHelpBar(createMockStyleProviderForHelpBar())
	hb.SetEnabled(true)

	shortcuts := []ui.KeyShortcut{
		{Key: "?", Description: "Help"},
	}

	hb.SetShortcuts(shortcuts)
	output := hb.Render()

	if !strings.Contains(output, "?") {
		t.Error("Expected output to contain single shortcut key")
	}

	if !strings.Contains(output, "Help") {
		t.Error("Expected output to contain single shortcut description")
	}
}
