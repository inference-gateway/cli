package components

import (
	"fmt"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
	uimocks "github.com/inference-gateway/cli/tests/mocks/ui"
	sdk "github.com/inference-gateway/sdk"
)

// createMockStyleProvider creates a mock styles provider for testing
func createMockStyleProvider() *styles.Provider {
	fakeTheme := &uimocks.FakeTheme{}
	fakeThemeService := &domainmocks.FakeThemeService{}
	fakeThemeService.GetCurrentThemeReturns(fakeTheme)
	return styles.NewProvider(fakeThemeService)
}

func TestNewConversationView(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

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
	cv := NewConversationView(createMockStyleProvider())

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Hello"),
			},
			Time: time.Now(),
		},
		{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("Hi there!"),
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

	contentStr, _ := cv.conversation[1].Message.Content.AsMessageContent0()
	if contentStr != "Hi there!" {
		t.Errorf("Expected second entry content 'Hi there!', got '%s'", contentStr)
	}
}

func TestConversationView_GetScrollOffset(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	offset := cv.GetScrollOffset()

	if offset != 0 {
		t.Errorf("Expected scroll offset 0, got %d", offset)
	}
}

func TestConversationView_CanScrollUp(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	if cv.CanScrollUp() {
		t.Error("Expected CanScrollUp to be false when at top")
	}
}

func TestConversationView_CanScrollDown(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	if cv.CanScrollDown() {
		t.Error("Expected CanScrollDown to be false with no content")
	}
}

func TestConversationView_ToggleToolResultExpansion(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Test message"),
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
	cv := NewConversationView(createMockStyleProvider())

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.Tool,
				Content: sdk.NewMessageContent("Tool result 1"),
			},
			Time: time.Now(),
		},
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("User message"),
			},
			Time: time.Now(),
		},
		{
			Message: sdk.Message{
				Role:    sdk.Tool,
				Content: sdk.NewMessageContent("Tool result 2"),
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
	cv := NewConversationView(createMockStyleProvider())

	if cv.IsToolResultExpanded(0) {
		t.Error("Expected tool result 0 to not be expanded initially")
	}

	if cv.IsToolResultExpanded(999) {
		t.Error("Expected non-existent tool result to not be expanded")
	}
}

func TestConversationView_SetWidth(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.SetWidth(120)

	if cv.width != 120 {
		t.Errorf("Expected width 120, got %d", cv.width)
	}

	if cv.Viewport.Width != 120 {
		t.Errorf("Expected viewport width 120, got %d", cv.Viewport.Width)
	}
}

func TestConversationView_SetHeight(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	cv.SetHeight(30)

	if cv.height != 30 {
		t.Errorf("Expected height 30, got %d", cv.height)
	}

	if cv.Viewport.Height != 30 {
		t.Errorf("Expected viewport height 30, got %d", cv.Viewport.Height)
	}
}

func TestConversationView_Render(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())

	output := cv.Render()

	if output == "" {
		t.Error("Expected non-empty render output")
	}

	conversation := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Test message"),
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

// TestConversationView_ConcurrentStreamingAccess tests thread-safety of streaming operations
func TestConversationView_ConcurrentStreamingAccess(t *testing.T) {
	cv := NewConversationView(createMockStyleProvider())
	cv.SetWidth(100)
	cv.SetHeight(30)

	done := make(chan bool)
	go func() {
		for i := 0; i < 1000; i++ {
			cv.appendStreamingContent(fmt.Sprintf("chunk %d ", i), "", "test-model")
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 1000; i++ {
			_ = cv.Render()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	<-done
	<-done

	cv.flushStreamingBuffer()

	cv.streamingMu.RLock()
	isStreaming := cv.isStreaming
	bufLen := cv.streamingBuffer.Len()
	cv.streamingMu.RUnlock()

	if isStreaming {
		t.Error("Expected streaming to be stopped after flush")
	}
	if bufLen != 0 {
		t.Errorf("Expected buffer length 0 after flush, got %d", bufLen)
	}
}
