package chat

import (
	"errors"
	"strings"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	colors "github.com/inference-gateway/cli/internal/presentation/tui/styles/colors"
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
	if NewCommand(nil).Flags().Lookup("session-id") == nil {
		t.Fatal("expected chat command to register a --session-id flag")
	}
}

func TestResumeChatSession(t *testing.T) {
	tests := []struct {
		name      string
		lease     agentdomain.Lease
		found     bool
		leaseErr  error
		loadErr   error
		wantErr   string
		wantLoads int
	}{
		{
			name:      "loads a session nobody holds a lease on",
			wantLoads: 1,
		},
		{
			name:      "loads a session this workspace owns",
			lease:     agentdomain.Lease{Workspace: "laptop", Until: time.Now().Add(time.Hour)},
			found:     true,
			wantLoads: 1,
		},
		{
			name:      "continues without panicking when loading fails",
			loadErr:   errors.New("not found"),
			wantLoads: 1,
		},
		{
			name:    "refuses a session another workspace owns",
			lease:   agentdomain.Lease{Workspace: "k8s-dev", Until: time.Now().Add(time.Hour)},
			found:   true,
			wantErr: "session abc-123-def runs on workspace k8s-dev",
		},
		{
			name:    "refuses a session whose lease another workspace let lapse",
			lease:   agentdomain.Lease{Workspace: "k8s-dev", Until: time.Now().Add(-time.Hour)},
			found:   true,
			wantErr: "infer chat --daemon --session-id abc-123-def",
		},
		{
			name:     "refuses when the lease cannot be read",
			leaseErr: errors.New("corrupt leases file"),
			wantErr:  "reading the lease of session abc-123-def: corrupt leases file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeRepo := &convmocks.FakeConversationRepository{}
			fakeRepo.LoadConversationReturns(tt.loadErr)
			leases := &agentdomainmocks.FakeLeaseStore{}
			leases.GetLeaseReturns(tt.lease, tt.found, tt.leaseErr)

			err := resumeChatSession(fakeRepo, nil, leases, "laptop", "abc-123-def")

			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("expected an error containing %q, got %v", tt.wantErr, err)
			}
			if got := fakeRepo.LoadConversationCallCount(); got != tt.wantLoads {
				t.Fatalf("expected LoadConversation to be called %d times, got %d", tt.wantLoads, got)
			}
			if _, id := leases.GetLeaseArgsForCall(0); id != "abc-123-def" {
				t.Errorf("expected the lease of abc-123-def, got %q", id)
			}
		})
	}

	t.Run("skips the lease check without a lease store", func(t *testing.T) {
		fakeRepo := &convmocks.FakeConversationRepository{}

		if err := resumeChatSession(fakeRepo, nil, nil, "laptop", "abc-123-def"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fakeRepo.LoadConversationCallCount() != 1 {
			t.Fatalf("expected LoadConversation to be called once, got %d", fakeRepo.LoadConversationCallCount())
		}
	})
}
