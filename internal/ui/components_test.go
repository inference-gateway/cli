package ui

import (
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestConversationViewScrolling(t *testing.T) {
	theme := NewDefaultTheme()
	layout := NewDefaultLayout()
	factory := NewComponentFactory(theme, layout, nil)

	cv := factory.CreateConversationView().(*ConversationViewImpl)
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
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 1 {
		t.Errorf("Expected scroll offset 1, got %d", cv.GetScrollOffset())
	}

	mouseMsg = tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	}
	model, _ = cv.Update(mouseMsg)
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Expected scroll offset 0, got %d", cv.GetScrollOffset())
	}

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

	scrollMsg = ScrollRequestMsg{
		ComponentID: "conversation",
		Direction:   ScrollToBottom,
		Amount:      0,
	}
	model, _ = cv.Update(scrollMsg)
	cv = model.(*ConversationViewImpl)

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
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Expected scroll offset 0 (top), got %d", cv.GetScrollOffset())
	}
}

func TestConversationViewScrollBounds(t *testing.T) {
	theme := NewDefaultTheme()
	layout := NewDefaultLayout()
	factory := NewComponentFactory(theme, layout, nil)

	cv := factory.CreateConversationView().(*ConversationViewImpl)
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
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Scroll offset should remain 0, got %d", cv.GetScrollOffset())
	}

	mouseMsg = tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	}
	model, _ = cv.Update(mouseMsg)
	cv = model.(*ConversationViewImpl)

	if cv.GetScrollOffset() != 0 {
		t.Errorf("Scroll offset should remain 0, got %d", cv.GetScrollOffset())
	}
}
