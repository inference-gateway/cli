package services

import (
	"encoding/json"
	"testing"

	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

func TestFilterSystemReminders(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil)

	userMessage := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: "Hello, how are you?",
		},
	}
	if err := repo.AddMessage(userMessage); err != nil {
		t.Fatalf("Failed to add user message: %v", err)
	}

	assistantMessage := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.Assistant,
			Content: "I'm doing well, thank you!",
		},
	}
	if err := repo.AddMessage(assistantMessage); err != nil {
		t.Fatalf("Failed to add assistant message: %v", err)
	}

	systemReminder := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: "<system-reminder>This is a reminder</system-reminder>",
		},
		IsSystemReminder: true,
	}
	if err := repo.AddMessage(systemReminder); err != nil {
		t.Fatalf("Failed to add system reminder: %v", err)
	}

	userMessage2 := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: "What's the weather like?",
		},
	}
	if err := repo.AddMessage(userMessage2); err != nil {
		t.Fatalf("Failed to add user message 2: %v", err)
	}

	allMessages := repo.GetMessages()
	if len(allMessages) != 4 {
		t.Errorf("Expected 4 total messages, got %d", len(allMessages))
	}

	jsonData, err := repo.Export(domain.ExportJSON)
	if err != nil {
		t.Fatalf("Failed to export JSON: %v", err)
	}

	var exportedMessages []domain.ConversationEntry
	if err := json.Unmarshal(jsonData, &exportedMessages); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(exportedMessages) != 3 {
		t.Errorf("Expected 3 exported messages (system reminder filtered), got %d", len(exportedMessages))
	}

	for _, msg := range exportedMessages {
		if msg.IsSystemReminder {
			t.Error("System reminder found in exported messages, should be filtered out")
		}
		if msg.Message.Content == "<system-reminder>This is a reminder</system-reminder>" {
			t.Error("System reminder content found in exported messages")
		}
	}

	markdownData, err := repo.Export(domain.ExportMarkdown)
	if err != nil {
		t.Fatalf("Failed to export markdown: %v", err)
	}

	markdownContent := string(markdownData)
	if len(markdownContent) == 0 {
		t.Error("Markdown export is empty")
	}

	if contains(markdownContent, "<system-reminder>This is a reminder</system-reminder>") {
		t.Error("System reminder content found in markdown export")
	}

	textData, err := repo.Export(domain.ExportText)
	if err != nil {
		t.Fatalf("Failed to export text: %v", err)
	}

	textContent := string(textData)
	if len(textContent) == 0 {
		t.Error("Text export is empty")
	}

	if contains(textContent, "<system-reminder>This is a reminder</system-reminder>") {
		t.Error("System reminder content found in text export")
	}
}

func TestFilterSystemRemindersEmpty(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil)

	systemReminder1 := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: "<system-reminder>Reminder 1</system-reminder>",
		},
		IsSystemReminder: true,
	}
	if err := repo.AddMessage(systemReminder1); err != nil {
		t.Fatalf("Failed to add system reminder 1: %v", err)
	}

	systemReminder2 := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: "<system-reminder>Reminder 2</system-reminder>",
		},
		IsSystemReminder: true,
	}
	if err := repo.AddMessage(systemReminder2); err != nil {
		t.Fatalf("Failed to add system reminder 2: %v", err)
	}

	jsonData, err := repo.Export(domain.ExportJSON)
	if err != nil {
		t.Fatalf("Failed to export JSON: %v", err)
	}

	var exportedMessages []domain.ConversationEntry
	if err := json.Unmarshal(jsonData, &exportedMessages); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(exportedMessages) != 0 {
		t.Errorf("Expected 0 exported messages (all were system reminders), got %d", len(exportedMessages))
	}
}

func TestFilterSystemRemindersWithEmptyRepo(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil)

	jsonData, err := repo.Export(domain.ExportJSON)
	if err != nil {
		t.Fatalf("Failed to export JSON: %v", err)
	}

	var exportedMessages []domain.ConversationEntry
	if err := json.Unmarshal(jsonData, &exportedMessages); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(exportedMessages) != 0 {
		t.Errorf("Expected 0 exported messages from empty repo, got %d", len(exportedMessages))
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		indexOfSubstring(s, substr) >= 0)
}

func indexOfSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
