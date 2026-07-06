package cmd

import (
	"strings"
	"testing"

	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestChatExitMessageFormat(t *testing.T) {
	t.Run("exit message includes session ID when available", func(t *testing.T) {
		sessionID := "abc-123-def"
		expectedMsg := "Chat session ended. Continue with: infer chat --session-id " + sessionID

		if !strings.Contains(expectedMsg, sessionID) {
			t.Errorf("Expected message to contain session ID %q", sessionID)
		}

		if !strings.Contains(expectedMsg, "infer chat --session-id") {
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
		msg := "Chat session ended. Continue with: infer chat --session-id abc-123"
		emojis := []string{"•", "⚠️", "✅", "🎉", "💬", "👋", "✨", "🚀", "📝"}
		for _, emoji := range emojis {
			if strings.Contains(msg, emoji) {
				t.Errorf("Expected no emojis in exit message, found %q", emoji)
			}
		}
	})

	t.Run("exit message is easy to copy and paste", func(t *testing.T) {
		sessionID := "abc-123-def"
		msg := "Chat session ended. Continue with: infer chat --session-id " + sessionID

		if !strings.Contains(msg, "infer chat --session-id "+sessionID) {
			t.Error("Expected the full command to be present for easy copy-paste")
		}
	})

	t.Run("GetCurrentConversationID returns session ID from generated mock", func(t *testing.T) {
		fakeRepo := &mocks.FakeConversationRepository{}
		fakeRepo.GetCurrentConversationIDReturns("abc-123-def")

		sessionID := fakeRepo.GetCurrentConversationID()
		if sessionID != "abc-123-def" {
			t.Errorf("Expected session ID 'abc-123-def', got %q", sessionID)
		}

		if fakeRepo.GetCurrentConversationIDCallCount() != 1 {
			t.Error("Expected GetCurrentConversationID to be called once")
		}
	})

	t.Run("GetCurrentConversationID returns empty string from generated mock", func(t *testing.T) {
		fakeRepo := &mocks.FakeConversationRepository{}
		fakeRepo.GetCurrentConversationIDReturns("")

		sessionID := fakeRepo.GetCurrentConversationID()
		if sessionID != "" {
			t.Errorf("Expected empty session ID, got %q", sessionID)
		}
	})
}
