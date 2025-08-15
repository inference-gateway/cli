package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestConversationViewScrolling(t *testing.T) {
	cv := NewConversationView()
	cv.SetWidth(80)
	cv.SetHeight(5)

	messages := make([]domain.ConversationEntry, 10)
	for i := 0; i < 10; i++ {
		messages[i] = domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: "Test message " + string(rune('0'+i)),
			},
		}
	}

	cv.SetConversation(messages)
	cv.viewport.GotoTop()

	if cv.CanScrollUp() {
		t.Error("Should not be able to scroll up initially")
	}
	if !cv.CanScrollDown() {
		t.Error("Should be able to scroll down with 10 messages and height 5")
	}

	mouseMsg := tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	}
	model, _ := cv.Update(mouseMsg)
	cv = model.(*ConversationView)

	if cv.GetScrollOffset() != 1 {
		t.Errorf("Expected scroll offset 1, got %d", cv.GetScrollOffset())
	}

	mouseMsg = tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	}
	model, _ = cv.Update(mouseMsg)
	cv = model.(*ConversationView)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Expected scroll offset 0, got %d", cv.GetScrollOffset())
	}

	scrollMsg := ScrollRequestMsg{
		ComponentID: "conversation",
		Direction:   ScrollDown,
		Amount:      3,
	}
	model, _ = cv.Update(scrollMsg)
	cv = model.(*ConversationView)

	if cv.GetScrollOffset() != 3 {
		t.Errorf("Expected scroll offset 3, got %d", cv.GetScrollOffset())
	}

	scrollMsg = ScrollRequestMsg{
		ComponentID: "conversation",
		Direction:   ScrollToBottom,
		Amount:      0,
	}
	model, _ = cv.Update(scrollMsg)
	cv = model.(*ConversationView)

	expectedBottom := cv.viewport.TotalLineCount() - cv.viewport.Height
	if cv.GetScrollOffset() != expectedBottom {
		t.Errorf("Expected scroll offset %d (bottom), got %d", expectedBottom, cv.GetScrollOffset())
	}

	scrollMsg = ScrollRequestMsg{
		ComponentID: "conversation",
		Direction:   ScrollToTop,
		Amount:      0,
	}
	model, _ = cv.Update(scrollMsg)
	cv = model.(*ConversationView)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Expected scroll offset 0 (top), got %d", cv.GetScrollOffset())
	}
}

func TestConversationViewScrollBounds(t *testing.T) {
	cv := NewConversationView()
	cv.SetWidth(80)
	cv.SetHeight(5)

	messages := make([]domain.ConversationEntry, 1)
	for i := 0; i < 1; i++ {
		messages[i] = domain.ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: "Test message " + string(rune('0'+i)),
			},
		}
	}

	cv.SetConversation(messages)
	cv.viewport.GotoTop()

	if cv.CanScrollUp() {
		t.Error("Should not be able to scroll up with fewer messages than height")
	}
	if cv.CanScrollDown() {
		t.Error("Should not be able to scroll down with fewer messages than height")
	}

	mouseMsg := tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	}
	model, _ := cv.Update(mouseMsg)
	cv = model.(*ConversationView)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Scroll offset should remain 0, got %d", cv.GetScrollOffset())
	}

	mouseMsg = tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	}
	model, _ = cv.Update(mouseMsg)
	cv = model.(*ConversationView)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Scroll offset should remain 0, got %d", cv.GetScrollOffset())
	}
}

func TestInputView(t *testing.T) {
	iv := NewInputView(nil)
	iv.SetWidth(80)
	iv.SetHeight(5)

	// Test input
	if iv.GetInput() != "" {
		t.Error("Initial input should be empty")
	}

	iv.SetText("hello world")
	if iv.GetInput() != "hello world" {
		t.Errorf("Expected 'hello world', got '%s'", iv.GetInput())
	}

	// Test cursor
	iv.SetCursor(5)
	if iv.GetCursor() != 5 {
		t.Errorf("Expected cursor at 5, got %d", iv.GetCursor())
	}

	// Test clear
	iv.ClearInput()
	if iv.GetInput() != "" {
		t.Error("Input should be empty after clear")
	}
	if iv.GetCursor() != 0 {
		t.Error("Cursor should be at 0 after clear")
	}
}

func TestStatusView(t *testing.T) {
	sv := NewStatusView()
	sv.SetWidth(80)

	// Test status
	sv.ShowStatus("Test status")
	if !strings.Contains(sv.Render(), "Test status") {
		t.Error("Status should contain test message")
	}

	// Test error
	sv.ShowError("Test error")
	if !sv.IsShowingError() {
		t.Error("Should be showing error")
	}
	if !strings.Contains(sv.Render(), "Test error") {
		t.Error("Error should contain test message")
	}

	// Test spinner
	sv.ShowSpinner("Loading...")
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

func TestHelpBar(t *testing.T) {
	hb := NewHelpBar()
	hb.SetWidth(80)

	shortcuts := []KeyShortcut{
		{Key: "!", Description: "Execute bash command"},
		{Key: "?", Description: "Toggle help"},
	}

	hb.SetShortcuts(shortcuts)
	if !hb.IsEnabled() {
		hb.SetEnabled(true)
	}

	rendered := hb.Render()
	if !strings.Contains(rendered, "Execute bash command") {
		t.Error("Help bar should contain shortcut descriptions")
	}
}

func TestLayoutCalculations(t *testing.T) {
	// Test conversation height calculation
	totalHeight := 20
	conversationHeight := CalculateConversationHeight(totalHeight)
	if conversationHeight < 3 {
		t.Error("Conversation height should be at least 3")
	}

	// Test input height calculation
	inputHeight := CalculateInputHeight(totalHeight)
	if inputHeight < 2 {
		t.Error("Input height should be at least 2")
	}

	// Test status height calculation
	statusHeight := CalculateStatusHeight(totalHeight)
	if statusHeight < 0 {
		t.Error("Status height should be non-negative")
	}

	// Test margins
	top, right, bottom, left := GetMargins()
	if top != 1 || right != 2 || bottom != 1 || left != 2 {
		t.Errorf("Expected margins (1,2,1,2), got (%d,%d,%d,%d)", top, right, bottom, left)
	}
}
