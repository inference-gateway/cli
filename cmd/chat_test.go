package cmd

import (
	"errors"
	"strings"
	"testing"

	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestChatExitMessage(t *testing.T) {
	t.Run("includes full continuation command when session ID is available", func(t *testing.T) {
		sessionID := "abc-123-def"
		msg := chatExitMessage(sessionID)

		if !strings.Contains(msg, "infer chat --session-id "+sessionID) {
			t.Errorf("expected full continuation command for copy-paste, got %q", msg)
		}
	})

	t.Run("plain message without session ID", func(t *testing.T) {
		if msg := chatExitMessage(""); msg != "Chat session ended." {
			t.Errorf("expected plain exit message, got %q", msg)
		}
	})

	t.Run("does not contain emojis", func(t *testing.T) {
		msg := chatExitMessage("abc-123")
		emojis := []string{"•", "⚠️", "✅", "🎉", "💬", "👋", "✨", "🚀", "📝"}
		for _, emoji := range emojis {
			if strings.Contains(msg, emoji) {
				t.Errorf("expected no emojis in exit message, found %q", emoji)
			}
		}
	})

	t.Run("exit message uses dim color", func(t *testing.T) {
		coloredText := colors.CreateColoredText(chatExitMessage(""), colors.DimColor)
		if !strings.HasPrefix(coloredText, colors.DimColor.ANSI) {
			t.Error("expected colored text to start with dim color ANSI code")
		}
		if !strings.HasSuffix(coloredText, colors.Reset) {
			t.Error("expected colored text to end with reset code")
		}
	})
}

func TestChatCommandSessionIDFlag(t *testing.T) {
	if chatCmd.Flags().Lookup("session-id") == nil {
		t.Fatal("expected chat command to register a --session-id flag")
	}
}

func TestResumeChatSession(t *testing.T) {
	t.Run("loads the requested conversation", func(t *testing.T) {
		fakeRepo := &mocks.FakeConversationRepository{}
		fakeRepo.LoadConversationReturns(nil)

		resumeChatSession(fakeRepo, nil, "abc-123-def")

		if fakeRepo.LoadConversationCallCount() != 1 {
			t.Fatalf("expected LoadConversation to be called once, got %d", fakeRepo.LoadConversationCallCount())
		}
		_, id := fakeRepo.LoadConversationArgsForCall(0)
		if id != "abc-123-def" {
			t.Errorf("expected conversation ID abc-123-def, got %q", id)
		}
	})

	t.Run("continues without panicking when loading fails", func(t *testing.T) {
		fakeRepo := &mocks.FakeConversationRepository{}
		fakeRepo.LoadConversationReturns(errors.New("not found"))

		resumeChatSession(fakeRepo, nil, "missing-id")

		if fakeRepo.LoadConversationCallCount() != 1 {
			t.Fatalf("expected LoadConversation to be called once, got %d", fakeRepo.LoadConversationCallCount())
		}
	})
}
