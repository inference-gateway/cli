package components

import (
	"strings"
	"testing"

	"github.com/inference-gateway/cli/internal/ui/shared"
)

func TestNewHelpBar(t *testing.T) {
	hb := NewHelpBar()

	if hb == nil {
		t.Fatal("Expected HelpBar to be created, got nil")
	}

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
	hb := NewHelpBar()

	shortcuts := []shared.KeyShortcut{
		{Key: "Ctrl+D", Description: "Send message"},
		{Key: "Ctrl+C", Description: "Cancel"},
		{Key: "↑↓", Description: "History"},
	}

	hb.SetShortcuts(shortcuts)

	if len(hb.shortcuts) != 3 {
		t.Errorf("Expected 3 shortcuts, got %d", len(hb.shortcuts))
	}

	if hb.shortcuts[0].Key != "Ctrl+D" {
		t.Errorf("Expected first shortcut key 'Ctrl+D', got '%s'", hb.shortcuts[0].Key)
	}

	if hb.shortcuts[1].Description != "Cancel" {
		t.Errorf("Expected second shortcut description 'Cancel', got '%s'", hb.shortcuts[1].Description)
	}
}

func TestHelpBar_IsEnabled(t *testing.T) {
	hb := NewHelpBar()

	if hb.IsEnabled() {
		t.Error("Expected help bar to be disabled by default")
	}
}

func TestHelpBar_SetEnabled(t *testing.T) {
	hb := NewHelpBar()

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
	hb := NewHelpBar()

	hb.SetWidth(120)

	if hb.width != 120 {
		t.Errorf("Expected width 120, got %d", hb.width)
	}
}

func TestHelpBar_SetHeight(t *testing.T) {
	hb := NewHelpBar()

	hb.SetHeight(2)
}

func TestHelpBar_Render_Disabled(t *testing.T) {
	hb := NewHelpBar()
	hb.SetEnabled(false)

	output := hb.Render()

	if output != "" {
		t.Errorf("Expected empty output when disabled, got '%s'", output)
	}
}

func TestHelpBar_Render_NoShortcuts(t *testing.T) {
	hb := NewHelpBar()
	hb.SetEnabled(true)

	output := hb.Render()

	if output != "" {
		t.Error("Expected empty output with no shortcuts")
	}
}

func TestHelpBar_Render_WithShortcuts(t *testing.T) {
	hb := NewHelpBar()
	hb.SetEnabled(true)

	shortcuts := []shared.KeyShortcut{
		{Key: "Ctrl+D", Description: "Send"},
		{Key: "Ctrl+C", Description: "Cancel"},
		{Key: "?", Description: "Help"},
	}

	hb.SetShortcuts(shortcuts)
	output := hb.Render()

	if output == "" {
		t.Error("Expected non-empty output with shortcuts")
	}

	if !strings.Contains(output, "Ctrl+D") {
		t.Error("Expected output to contain 'Ctrl+D'")
	}

	if !strings.Contains(output, "Send") {
		t.Error("Expected output to contain 'Send'")
	}

	if !strings.Contains(output, "Cancel") {
		t.Error("Expected output to contain 'Cancel'")
	}
}

func TestHelpBar_Render_LongShortcuts(t *testing.T) {
	hb := NewHelpBar()
	hb.SetEnabled(true)
	hb.SetWidth(20)

	shortcuts := []shared.KeyShortcut{
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
	hb := NewHelpBar()
	hb.SetEnabled(true)

	hb.SetShortcuts([]shared.KeyShortcut{})
	output := hb.Render()

	if output != "" {
		t.Error("Expected empty output with empty shortcuts array")
	}
}

func TestHelpBar_Render_SingleShortcut(t *testing.T) {
	hb := NewHelpBar()
	hb.SetEnabled(true)

	shortcuts := []shared.KeyShortcut{
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
