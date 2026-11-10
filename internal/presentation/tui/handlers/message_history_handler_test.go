package handlers

import (
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

func TestMessageHistoryHandler_HandleEditSubmit_FirstMessage(t *testing.T) {
	repo := conversation.NewInMemoryConversationRepository(nil, nil)
	handler := NewMessageHistoryHandler(repo)

	messages := []convdomain.ConversationEntry{
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

	event := tui.MessageEditSubmitEvent{
		RequestID:     "test-request",
		OriginalIndex: 0,
		EditedContent: "Edited first message",
		Images:        nil,
	}

	cmd := handler.HandleEditSubmit(event)
	msg := cmd()

	userInputEvent, ok := msg.(agentdomain.UserInputEvent)
	if !ok {
		t.Fatalf("Expected UserInputEvent but got: %T", msg)
	}

	if userInputEvent.Content != "Edited first message" {
		t.Errorf("Expected content 'Edited first message', got '%s'", userInputEvent.Content)
	}

	remainingMessages := repo.GetMessages()
	if len(remainingMessages) != 0 {
		t.Errorf("Expected the edited message and everything after it to be gone, got %d", len(remainingMessages))
	}
}

func TestMessageHistoryHandler_HandleEditSubmit_MiddleMessage(t *testing.T) {
	repo := conversation.NewInMemoryConversationRepository(nil, nil)
	handler := NewMessageHistoryHandler(repo)

	messages := []convdomain.ConversationEntry{
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

	event := tui.MessageEditSubmitEvent{
		RequestID:     "test-request",
		OriginalIndex: 2,
		EditedContent: "Edited second message",
		Images:        nil,
	}

	cmd := handler.HandleEditSubmit(event)
	msg := cmd()

	userInputEvent, ok := msg.(agentdomain.UserInputEvent)
	if !ok {
		t.Fatalf("Expected UserInputEvent but got: %T", msg)
	}

	if userInputEvent.Content != "Edited second message" {
		t.Errorf("Expected content 'Edited second message', got '%s'", userInputEvent.Content)
	}

	remainingMessages := repo.GetMessages()
	expectedCount := 2
	if len(remainingMessages) != expectedCount {
		t.Errorf("Expected %d messages before the edited one, got %d", expectedCount, len(remainingMessages))
	}
}
