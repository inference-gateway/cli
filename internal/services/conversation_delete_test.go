package services

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestInMemoryConversationRepository_DeleteMessagesAfterIndex(t *testing.T) {
	tests := []struct {
		name          string
		initialCount  int
		deleteIndex   int
		expectedCount int
		wantErr       bool
	}{
		{
			name:          "Delete from middle",
			initialCount:  10,
			deleteIndex:   5,
			expectedCount: 6,
			wantErr:       false,
		},
		{
			name:          "Delete from first message",
			initialCount:  10,
			deleteIndex:   0,
			expectedCount: 1,
			wantErr:       false,
		},
		{
			name:          "Delete from last message",
			initialCount:  10,
			deleteIndex:   9,
			expectedCount: 10,
			wantErr:       false,
		},
		{
			name:          "Delete with negative index",
			initialCount:  10,
			deleteIndex:   -1,
			expectedCount: 10,
			wantErr:       true,
		},
		{
			name:          "Delete with out of range index",
			initialCount:  10,
			deleteIndex:   15,
			expectedCount: 10,
			wantErr:       true,
		},
		{
			name:          "Delete from single message",
			initialCount:  1,
			deleteIndex:   0,
			expectedCount: 1,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := NewInMemoryConversationRepository(nil, nil)

			for i := 0; i < tt.initialCount; i++ {
				msg := domain.ConversationEntry{
					Time: time.Now(),
					Message: sdk.Message{
						Role: sdk.User,
						Content: sdk.NewMessageContent(
							"test message " + string(rune('A'+i)),
						),
					},
				}
				if err := repo.AddMessage(msg); err != nil {
					t.Fatalf("Failed to add message: %v", err)
				}
			}

			if len(repo.GetMessages()) != tt.initialCount {
				t.Fatalf("Initial message count = %d, want %d", len(repo.GetMessages()), tt.initialCount)
			}

			err := repo.DeleteMessagesAfterIndex(tt.deleteIndex)

			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteMessagesAfterIndex() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			finalCount := len(repo.GetMessages())
			if finalCount != tt.expectedCount {
				t.Errorf("After deletion: message count = %d, want %d", finalCount, tt.expectedCount)
			}

			if !tt.wantErr && finalCount > 0 {
				messages := repo.GetMessages()
				for i := 0; i <= tt.deleteIndex && i < len(messages); i++ {
					content, err := messages[i].Message.Content.AsMessageContent0()
					if err != nil {
						t.Fatalf("Failed to get message content: %v", err)
					}
					expected := "test message " + string(rune('A'+i))
					if content != expected {
						t.Errorf("Message %d content = %q, want %q", i, content, expected)
					}
				}
			}
		})
	}
}

func TestInMemoryConversationRepository_DeleteMessagesAfterIndex_PreservesMetadata(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	type testMessage struct {
		role    sdk.MessageRole
		content string
	}

	messages := []testMessage{
		{sdk.User, "User message 1"},
		{sdk.Assistant, "Assistant message 1"},
		{sdk.User, "User message 2"},
		{sdk.Assistant, "Assistant message 2"},
		{sdk.User, "User message 3"},
	}

	for _, msg := range messages {
		if err := repo.AddMessage(domain.ConversationEntry{
			Time: time.Now(),
			Message: sdk.Message{
				Role:    msg.role,
				Content: sdk.NewMessageContent(msg.content),
			},
		}); err != nil {
			t.Fatalf("Failed to add message: %v", err)
		}
	}

	err := repo.DeleteMessagesAfterIndex(2)
	if err != nil {
		t.Fatalf("DeleteMessagesAfterIndex() error = %v", err)
	}

	remaining := repo.GetMessages()
	if len(remaining) != 3 {
		t.Fatalf("Expected 3 messages, got %d", len(remaining))
	}

	if remaining[0].Message.Role != sdk.User {
		t.Errorf("Message 0 role = %v, want %v", remaining[0].Message.Role, sdk.User)
	}

	if remaining[1].Message.Role != sdk.Assistant {
		t.Errorf("Message 1 role = %v, want %v", remaining[1].Message.Role, sdk.Assistant)
	}

	if remaining[2].Message.Role != sdk.User {
		t.Errorf("Message 2 role = %v, want %v", remaining[2].Message.Role, sdk.User)
	}
}

func TestInMemoryConversationRepository_DeleteMessagesAfterIndex_EmptyRepo(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	err := repo.DeleteMessagesAfterIndex(0)
	if err == nil {
		t.Error("DeleteMessagesAfterIndex() on empty repo should return error")
	}
}

func TestInMemoryConversationRepository_DeleteMessagesAfterIndex_ThreadSafety(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	for i := 0; i < 100; i++ {
		if err := repo.AddMessage(domain.ConversationEntry{
			Time: time.Now(),
			Message: sdk.Message{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("message"),
			},
		}); err != nil {
			t.Fatalf("Failed to add message: %v", err)
		}
	}

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				_ = repo.GetMessages()
				time.Sleep(time.Millisecond)
			}
			done <- true
		}()
	}

	go func() {
		for i := 90; i >= 50; i -= 10 {
			_ = repo.DeleteMessagesAfterIndex(i)
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()

	for i := 0; i < 11; i++ {
		<-done
	}

	messages := repo.GetMessages()
	if messages == nil {
		t.Error("GetMessages returned nil after concurrent operations")
	}
}

func TestInMemoryConversationRepository_DeleteMessagesAfterIndex_BoundaryConditions(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)

	if err := repo.AddMessage(domain.ConversationEntry{
		Time: time.Now(),
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent("first"),
		},
	}); err != nil {
		t.Fatalf("Failed to add first message: %v", err)
	}
	if err := repo.AddMessage(domain.ConversationEntry{
		Time: time.Now(),
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: sdk.NewMessageContent("second"),
		},
	}); err != nil {
		t.Fatalf("Failed to add second message: %v", err)
	}

	err := repo.DeleteMessagesAfterIndex(0)
	if err != nil {
		t.Fatalf("DeleteMessagesAfterIndex(0) error = %v", err)
	}

	messages := repo.GetMessages()
	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}

	content, err := messages[0].Message.Content.AsMessageContent0()
	if err != nil {
		t.Fatalf("Failed to get content: %v", err)
	}
	if content != "first" {
		t.Errorf("Message content = %q, want %q", content, "first")
	}
}
