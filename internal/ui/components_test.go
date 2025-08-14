package ui

import (
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestConversationViewScrolling(t *testing.T) {
	theme := NewDefaultTheme()
	cv := &ConversationViewImpl{
		theme:        theme,
		conversation: []domain.ConversationEntry{},
		scrollOffset: 0,
		width:        80,
		height:       5, // Small height to test scrolling
	}

	// Create test messages
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

	// Test initial state
	if cv.CanScrollUp() {
		t.Error("Should not be able to scroll up initially")
	}
	if !cv.CanScrollDown() {
		t.Error("Should be able to scroll down with 10 messages and height 5")
	}

	// Test mouse wheel down
	mouseMsg := tea.MouseMsg{Type: tea.MouseWheelDown}
	model, _ := cv.Update(mouseMsg)
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 1 {
		t.Errorf("Expected scroll offset 1, got %d", cv.GetScrollOffset())
	}

	// Test mouse wheel up
	mouseMsg = tea.MouseMsg{Type: tea.MouseWheelUp}
	model, _ = cv.Update(mouseMsg)
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Expected scroll offset 0, got %d", cv.GetScrollOffset())
	}

	// Test scroll request message - scroll down
	scrollMsg := ScrollRequestMsg{
		ComponentID: "conversation",
		Direction:   ScrollDown,
		Amount:      3,
	}
	model, _ = cv.Update(scrollMsg)
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 3 {
		t.Errorf("Expected scroll offset 3, got %d", cv.GetScrollOffset())
	}

	// Test scroll to bottom
	scrollMsg = ScrollRequestMsg{
		ComponentID: "conversation",
		Direction:   ScrollToBottom,
		Amount:      0,
	}
	model, _ = cv.Update(scrollMsg)
	cv = model.(*ConversationViewImpl)

	expectedBottom := len(messages) - cv.height
	if cv.GetScrollOffset() != expectedBottom {
		t.Errorf("Expected scroll offset %d (bottom), got %d", expectedBottom, cv.GetScrollOffset())
	}

	// Test scroll to top
	scrollMsg = ScrollRequestMsg{
		ComponentID: "conversation",
		Direction:   ScrollToTop,
		Amount:      0,
	}
	model, _ = cv.Update(scrollMsg)
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Expected scroll offset 0 (top), got %d", cv.GetScrollOffset())
	}
}

func TestConversationViewScrollBounds(t *testing.T) {
	theme := NewDefaultTheme()
	cv := &ConversationViewImpl{
		theme:        theme,
		conversation: []domain.ConversationEntry{},
		scrollOffset: 0,
		width:        80,
		height:       5,
	}

	// Create fewer messages than height
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

	// Should not be able to scroll in either direction
	if cv.CanScrollUp() {
		t.Error("Should not be able to scroll up with fewer messages than height")
	}
	if cv.CanScrollDown() {
		t.Error("Should not be able to scroll down with fewer messages than height")
	}

	// Mouse wheel events should have no effect
	mouseMsg := tea.MouseMsg{Type: tea.MouseWheelDown}
	model, _ := cv.Update(mouseMsg)
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Scroll offset should remain 0, got %d", cv.GetScrollOffset())
	}

	mouseMsg = tea.MouseMsg{Type: tea.MouseWheelUp}
	model, _ = cv.Update(mouseMsg)
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Scroll offset should remain 0, got %d", cv.GetScrollOffset())
	}
}
