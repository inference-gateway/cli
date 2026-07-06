package cmd

import (
	"strings"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	sdk "github.com/inference-gateway/sdk"
)

// mockConversationRepo implements domain.ConversationRepository for testing.
type mockConversationRepo struct {
	conversationID string
	messages       []domain.ConversationEntry
}

func (m *mockConversationRepo) GetCurrentConversationID() string {
	return m.conversationID
}

func (m *mockConversationRepo) GetMessages() []domain.ConversationEntry {
	return m.messages
}

func (m *mockConversationRepo) AddMessage(entry domain.ConversationEntry) error {
	m.messages = append(m.messages, entry)
	return nil
}

func (m *mockConversationRepo) GetMessageCount() int {
	return len(m.messages)
}

func (m *mockConversationRepo) Clear() error {
	m.messages = nil
	return nil
}

func (m *mockConversationRepo) ClearExceptFirstUserMessage() error {
	return nil
}

func (m *mockConversationRepo) UpdateLastMessage(content string) error {
	return nil
}

func (m *mockConversationRepo) UpdateLastMessageToolCalls(toolCalls *[]sdk.ChatCompletionMessageToolCall) error {
	return nil
}

func (m *mockConversationRepo) DeleteMessagesAfterIndex(index int) error {
	return nil
}

func (m *mockConversationRepo) AddTokenUsage(model string, inputTokens, outputTokens, totalTokens int) error {
	return nil
}

func (m *mockConversationRepo) GetSessionTokens() domain.SessionTokenStats {
	return domain.SessionTokenStats{}
}

func (m *mockConversationRepo) GetSessionCostStats() domain.SessionCostStats {
	return domain.SessionCostStats{}
}

func (m *mockConversationRepo) GetCurrentConversationTitle() string {
	return "test"
}

func (m *mockConversationRepo) StartNewConversation(title string) error {
	return nil
}

func (m *mockConversationRepo) Export(format domain.ExportFormat) ([]byte, error) {
	return nil, nil
}

func (m *mockConversationRepo) RemovePendingToolCallByID(toolCallID string) {
}

func (m *mockConversationRepo) FormatToolResultForLLM(result *domain.ToolExecutionResult) string {
	return ""
}

func (m *mockConversationRepo) FormatToolResultForUI(result *domain.ToolExecutionResult, terminalWidth int) string {
	return ""
}

func (m *mockConversationRepo) FormatToolResultExpanded(result *domain.ToolExecutionResult, terminalWidth int) string {
	return ""
}

func TestChatExitMessageFormat(t *testing.T) {
	t.Run("exit message includes session ID when available", func(t *testing.T) {
		sessionID := "abc-123-def"
		expectedMsg := "Chat session ended. Continue with: infer agent --session-id " + sessionID

		// Verify the message contains the session ID
		if !strings.Contains(expectedMsg, sessionID) {
			t.Errorf("Expected message to contain session ID %q", sessionID)
		}

		// Verify the message contains the continuation instruction
		if !strings.Contains(expectedMsg, "infer agent --session-id") {
			t.Error("Expected message to contain continuation instruction")
		}
	})

	t.Run("exit message uses dim color", func(t *testing.T) {
		coloredText := colors.CreateColoredText("Chat session ended.", colors.DimColor)
		if !strings.HasPrefix(coloredText, colors.DimColor.ANSI) {
			t.Error("Expected colored text to start with dim color ANSI code")
		}
		if !strings.HasSuffix(coloredText, colors.Reset) {
			t.Error("Expected colored text to end with reset code")
		}
	})

	t.Run("exit message does not contain emojis", func(t *testing.T) {
		msg := "Chat session ended. Continue with: infer agent --session-id abc-123"
		emojis := []string{"•", "⚠️", "✅", "🎉", "💬", "👋", "✨", "🚀", "📝"}
		for _, emoji := range emojis {
			if strings.Contains(msg, emoji) {
				t.Errorf("Expected no emojis in exit message, found %q", emoji)
			}
		}
	})

	t.Run("exit message is easy to copy and paste", func(t *testing.T) {
		sessionID := "abc-123-def"
		msg := "Chat session ended. Continue with: infer agent --session-id " + sessionID

		if !strings.Contains(msg, "infer agent --session-id "+sessionID) {
			t.Error("Expected the full command to be present for easy copy-paste")
		}
	})
}
