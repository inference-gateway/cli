package handlers

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestMessageHistoryHandler_HandleEditSubmit_FirstMessage(t *testing.T) {
	repo := services.NewInMemoryConversationRepository(nil, nil)
	stateManager := &mocks.FakeStateManager{}
	handler := NewMessageHistoryHandler(stateManager, repo)

	messages := []domain.ConversationEntry{
		{
			Time:    time.Now(),
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("First user message")},
		},
		{
			Time:    time.Now(),
			Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("First assistant response")},
		},
		{
			Time:    time.Now(),
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Second user message")},
		},
		{
			Time:    time.Now(),
			Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("Second assistant response")},
		},
	}

	for _, msg := range messages {
		if err := repo.AddMessage(msg); err != nil {
			t.Fatalf("Failed to add message: %v", err)
		}
	}

	event := domain.MessageEditSubmitEvent{
		RequestID:     "test-request",
		OriginalIndex: 0,
		EditedContent: "Edited first message",
		Images:        nil,
	}

	cmd := handler.HandleEditSubmit(event)
	msg := cmd()

	userInputEvent, ok := msg.(domain.UserInputEvent)
	if !ok {
		t.Fatalf("Expected UserInputEvent but got: %T", msg)
	}

	if userInputEvent.Content != "Edited first message" {
		t.Errorf("Expected content 'Edited first message', got '%s'", userInputEvent.Content)
	}

	remainingMessages := repo.GetMessages()
	if len(remainingMessages) != 4 {
		t.Errorf("Expected 4 messages (deletion happens in app layer), got %d", len(remainingMessages))
	}
}

func TestMessageHistoryHandler_HandleEditSubmit_MiddleMessage(t *testing.T) {
	repo := services.NewInMemoryConversationRepository(nil, nil)
	stateManager := &mocks.FakeStateManager{}
	handler := NewMessageHistoryHandler(stateManager, repo)

	messages := []domain.ConversationEntry{
		{
			Time:    time.Now(),
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("First")},
		},
		{
			Time:    time.Now(),
			Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("Response 1")},
		},
		{
			Time:    time.Now(),
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Second")},
		},
		{
			Time:    time.Now(),
			Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("Response 2")},
		},
	}

	for _, msg := range messages {
		if err := repo.AddMessage(msg); err != nil {
			t.Fatalf("Failed to add message: %v", err)
		}
	}

	event := domain.MessageEditSubmitEvent{
		RequestID:     "test-request",
		OriginalIndex: 2,
		EditedContent: "Edited second message",
		Images:        nil,
	}

	cmd := handler.HandleEditSubmit(event)
	msg := cmd()

	userInputEvent, ok := msg.(domain.UserInputEvent)
	if !ok {
		t.Fatalf("Expected UserInputEvent but got: %T", msg)
	}

	if userInputEvent.Content != "Edited second message" {
		t.Errorf("Expected content 'Edited second message', got '%s'", userInputEvent.Content)
	}

	remainingMessages := repo.GetMessages()
	expectedCount := 4
	if len(remainingMessages) != expectedCount {
		t.Errorf("Expected %d messages (deletion happens in app layer), got %d", expectedCount, len(remainingMessages))
	}
}
