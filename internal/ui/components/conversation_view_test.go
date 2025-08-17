package components

import (
	"testing"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestNewConversationView(t *testing.T) {
	cv := NewConversationView()

	if cv == nil {
		t.Fatal("Expected ConversationView to be created, got nil")
	}

	if cv.width != 80 {
		t.Errorf("Expected default width 80, got %d", cv.width)
	}

	if cv.height != 20 {
		t.Errorf("Expected default height 20, got %d", cv.height)
	}

	if cv.expandedToolResults == nil {
		t.Error("Expected expandedToolResults to be initialized")
	}

	if cv.allToolsExpanded {
		t.Error("Expected allToolsExpanded to be false")
	}

	if len(cv.conversation) != 0 {
		t.Errorf("Expected empty conversation, got length %d", len(cv.conversation))
	}
}

func TestConversationView_SetConversation(t *testing.T) {
	cv := NewConversationView()

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: "Hello",
			},
			Time: time.Now(),
		},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: "Hi there!",
			},
			Time: time.Now(),
		},
	}

	cv.SetConversation(conversation)

	if len(cv.conversation) != 2 {
		t.Errorf("Expected conversation length 2, got %d", len(cv.conversation))
	}

	if cv.conversation[0].Message.Role != sdk.User {
		t.Errorf("Expected first entry role 'user', got '%s'", cv.conversation[0].Message.Role)
	}

	if cv.conversation[1].Message.Content != "Hi there!" {
		t.Errorf("Expected second entry content 'Hi there!', got '%s'", cv.conversation[1].Message.Content)
	}
}

func TestConversationView_GetScrollOffset(t *testing.T) {
	cv := NewConversationView()

	offset := cv.GetScrollOffset()

	if offset != 0 {
		t.Errorf("Expected scroll offset 0, got %d", offset)
	}
}

func TestConversationView_CanScrollUp(t *testing.T) {
	cv := NewConversationView()

	if cv.CanScrollUp() {
		t.Error("Expected CanScrollUp to be false when at top")
	}
}

func TestConversationView_CanScrollDown(t *testing.T) {
	cv := NewConversationView()

	if cv.CanScrollDown() {
		t.Error("Expected CanScrollDown to be false with no content")
	}
}

func TestConversationView_ToggleToolResultExpansion(t *testing.T) {
	cv := NewConversationView()

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: "Test message",
			},
			Time: time.Now(),
		},
	}
	cv.SetConversation(conversation)

	cv.ToggleToolResultExpansion(0)

	if !cv.IsToolResultExpanded(0) {
		t.Error("Expected tool result 0 to be expanded after toggle")
	}

	cv.ToggleToolResultExpansion(0)

	if cv.IsToolResultExpanded(0) {
		t.Error("Expected tool result 0 to be collapsed after second toggle")
	}
}

func TestConversationView_ToggleAllToolResultsExpansion(t *testing.T) {
	cv := NewConversationView()

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.Tool,
				Content: "Tool result 1",
			},
			Time: time.Now(),
		},
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: "User message",
			},
			Time: time.Now(),
		},
		{
			Message: sdk.Message{
				Role:    sdk.Tool,
				Content: "Tool result 2",
			},
			Time: time.Now(),
		},
	}
	cv.SetConversation(conversation)

	if cv.IsToolResultExpanded(0) || cv.IsToolResultExpanded(2) {
		t.Error("Expected all tool results to be collapsed initially")
	}

	cv.ToggleAllToolResultsExpansion()

	if !cv.IsToolResultExpanded(0) || !cv.IsToolResultExpanded(2) {
		t.Error("Expected all tool results to be expanded after first toggle")
	}

	if cv.IsToolResultExpanded(1) {
		t.Error("Expected non-tool message to remain unaffected")
	}

	cv.ToggleAllToolResultsExpansion()

	if cv.IsToolResultExpanded(0) || cv.IsToolResultExpanded(2) {
		t.Error("Expected all tool results to be collapsed after second toggle")
	}
}

func TestConversationView_IsToolResultExpanded(t *testing.T) {
	cv := NewConversationView()

	if cv.IsToolResultExpanded(0) {
		t.Error("Expected tool result 0 to not be expanded initially")
	}

	if cv.IsToolResultExpanded(999) {
		t.Error("Expected non-existent tool result to not be expanded")
	}
}

func TestConversationView_SetWidth(t *testing.T) {
	cv := NewConversationView()

	cv.SetWidth(120)

	if cv.width != 120 {
		t.Errorf("Expected width 120, got %d", cv.width)
	}

	if cv.Viewport.Width != 120 {
		t.Errorf("Expected viewport width 120, got %d", cv.Viewport.Width)
	}
}

func TestConversationView_SetHeight(t *testing.T) {
	cv := NewConversationView()

	cv.SetHeight(30)

	if cv.height != 30 {
		t.Errorf("Expected height 30, got %d", cv.height)
	}

	if cv.Viewport.Height != 30 {
		t.Errorf("Expected viewport height 30, got %d", cv.Viewport.Height)
	}
}

func TestConversationView_Render(t *testing.T) {
	cv := NewConversationView()

	output := cv.Render()

	if output == "" {
		t.Error("Expected non-empty render output")
	}

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: "Test message",
			},
			Time: time.Now(),
		},
	}

	cv.SetConversation(conversation)
	output = cv.Render()

	if output == "" {
		t.Error("Expected non-empty render output with conversation")
	}
}
