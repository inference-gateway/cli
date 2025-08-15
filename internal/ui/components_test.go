package ui

import (
	"strings"
	"testing"

	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestConversationViewBasic(t *testing.T) {
	cv := CreateConversationView()
	cv.SetWidth(80)
	cv.SetHeight(5)

	messages := make([]domain.ConversationEntry, 3)
	for i := 0; i < 3; i++ {
		messages[i] = domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: "Test message " + string(rune('0'+i)),
			},
		}
	}

	cv.SetConversation(messages)

	// Test basic functionality
	if cv.GetScrollOffset() < 0 {
		t.Error("Scroll offset should be non-negative")
	}

	// Test rendering doesn't crash
	output := cv.Render()
	if output == "" {
		t.Error("Render should return non-empty content")
	}
}

func TestInputViewBasic(t *testing.T) {
	iv := CreateInputView(nil, nil)
	iv.SetWidth(80)
	iv.SetHeight(5)

	// Test input functionality
	iv.SetPlaceholder("Test placeholder")
	if iv.GetInput() != "" {
		t.Error("Initial input should be empty")
	}

	// Test rendering doesn't crash
	output := iv.Render()
	if output == "" {
		t.Error("Render should return non-empty content")
	}

	// Test clear functionality
	iv.ClearInput()
	if iv.GetInput() != "" {
		t.Error("Input should be empty after clear")
	}
	if iv.GetCursor() != 0 {
		t.Error("Cursor should be at 0 after clear")
	}
}

func TestStatusViewBasic(t *testing.T) {
	sv := CreateStatusView()
	sv.SetWidth(80)

	// Test status
	sv.ShowStatus("Test status")
	if sv.IsShowingError() {
		t.Error("Should not be showing error when showing status")
	}
	if sv.IsShowingSpinner() {
		t.Error("Should not be showing spinner when showing status")
	}

	// Test error
	sv.ShowError("Test error")
	if !sv.IsShowingError() {
		t.Error("Should be showing error")
	}

	// Test spinner
	sv.ShowSpinner("Test spinner")
	if !sv.IsShowingSpinner() {
		t.Error("Should be showing spinner")
	}

	// Test clear
	sv.ClearStatus()
	if sv.IsShowingError() {
		t.Error("Should not be showing error after clear")
	}
	if sv.IsShowingSpinner() {
		t.Error("Should not be showing spinner after clear")
	}
}

func TestHelpBarBasic(t *testing.T) {
	hb := CreateHelpBar()
	hb.SetWidth(80)

	shortcuts := []KeyShortcut{
		{Key: "!", Description: "Execute bash command"},
		{Key: "?", Description: "Show help"},
	}

	hb.SetShortcuts(shortcuts)

	if hb.IsEnabled() {
		t.Error("Help bar should be disabled by default")
	}

	hb.SetEnabled(true)
	if !hb.IsEnabled() {
		t.Error("Help bar should be enabled after SetEnabled(true)")
	}

	// Test rendering doesn't crash
	output := hb.Render()
	if !strings.Contains(output, "!") {
		t.Error("Rendered output should contain shortcut keys")
	}
}