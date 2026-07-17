package services

import (
	"context"
	"strings"
	"testing"

	uuid "github.com/google/uuid"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	fakesdomain "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
)

// cleanerChannel combines the generated Channel and HistoryCleaner fakes so the
// manager's type assertion to domain.HistoryCleaner succeeds.
type cleanerChannel struct {
	*fakesdomain.FakeChannel
	*fakesdomain.FakeHistoryCleaner
}

func newCommandTestManager(t *testing.T) (*ChannelManagerService, *cleanerChannel, *storage.MemoryStorage) {
	t.Helper()

	reg := shortcuts.NewRegistry()
	reg.Register(shortcuts.NewClearShortcut(nil, nil))
	reg.Register(shortcuts.NewNewShortcut(nil, nil))
	reg.Register(shortcuts.NewHelpShortcut(reg))
	reg.Register(shortcuts.NewContextShortcut(nil, nil, nil))
	reg.Register(shortcuts.NewStatsShortcut())
	reg.Register(shortcuts.NewConversationSelectShortcut(nil))

	store := storage.NewMemoryStorage()
	cm := NewChannelManagerService(config.ChannelsConfig{Enabled: true}, nil)
	cm.SetCommandSupport(reg, store, store)

	ch := &cleanerChannel{
		FakeChannel:        &fakesdomain.FakeChannel{},
		FakeHistoryCleaner: &fakesdomain.FakeHistoryCleaner{},
	}
	ch.NameReturns("telegram")
	cm.Register(ch)
	return cm, ch, store
}

func TestParseChannelCommand(t *testing.T) {
	cm, _, _ := newCommandTestManager(t)

	tests := []struct {
		name     string
		content  string
		wantName string
		wantOK   bool
	}{
		{"registered command", "/clear", "clear", true},
		{"bot suffix stripped", "/clear@MyBot", "clear", true},
		{"uppercase lowered", "/CLEAR", "clear", true},
		{"args parsed", `/new "My Title"`, "new", true},
		{"unknown command falls through", "/unknown", "", false},
		{"bare path falls through", "/tmp/shot.png please look", "", false},
		{"plain text", "hello", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, _, ok := cm.parseChannelCommand(tt.content)
			if ok != tt.wantOK || name != tt.wantName {
				t.Fatalf("parseChannelCommand(%q) = (%q, %v), want (%q, %v)", tt.content, name, ok, tt.wantName, tt.wantOK)
			}
		})
	}

	t.Run("nil registry disables feature", func(t *testing.T) {
		cm := NewChannelManagerService(config.ChannelsConfig{Enabled: true}, nil)
		if _, _, ok := cm.parseChannelCommand("/clear"); ok {
			t.Fatal("expected commands to be disabled without a registry")
		}
	})
}

func TestHandleCommand_New(t *testing.T) {
	cm, ch, store := newCommandTestManager(t)
	ctx := context.Background()

	groupKey := domain.FormatChannelSessionID("telegram", "42")
	oldID := uuid.NewString()
	if err := store.SaveConversation(ctx, oldID, nil, storage.ConversationMetadata{}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSessionGroup(ctx, groupKey, storage.SessionGroupEntry{CurrentSessionID: oldID}); err != nil {
		t.Fatal(err)
	}

	cm.handleCommand(ctx, domain.InboundMessage{ChannelName: "telegram", SenderID: "42", Content: "/new"}, "new", nil)

	if _, _, err := store.LoadConversation(ctx, oldID); err != nil {
		t.Fatal("expected old conversation to be kept")
	}
	entry, _, _ := store.GetSessionGroup(ctx, groupKey)
	if entry.CurrentSessionID == oldID {
		t.Fatal("expected group repointed to a fresh session")
	}
	if _, err := uuid.Parse(entry.CurrentSessionID); err != nil {
		t.Fatalf("expected fresh UUID session ID, got %q", entry.CurrentSessionID)
	}
	if len(entry.History) != 1 || entry.History[0] != oldID {
		t.Fatalf("expected old session in history, got %v", entry.History)
	}
	if ch.ClearHistoryCallCount() != 1 {
		t.Fatalf("expected /new to wipe the chat once, got %d calls", ch.ClearHistoryCallCount())
	}
	if _, recipient := ch.ClearHistoryArgsForCall(0); recipient != "42" {
		t.Fatalf("expected ClearHistory for sender 42, got %q", recipient)
	}
	if ch.SendCallCount() != 1 {
		t.Fatalf("expected 1 confirmation send, got %d", ch.SendCallCount())
	}
	_, out := ch.SendArgsForCall(0)
	if !strings.Contains(out.Content, "fresh") {
		t.Fatalf("unexpected confirmation: %q", out.Content)
	}
}

func TestHandleCommand_Stats(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate config.TelemetryDir()
	cm, ch, _ := newCommandTestManager(t)

	cm.handleCommand(context.Background(), domain.InboundMessage{ChannelName: "telegram", SenderID: "42", Content: "/stats"}, "stats", nil)

	if ch.SendCallCount() != 1 {
		t.Fatalf("expected 1 send, got %d", ch.SendCallCount())
	}
	_, out := ch.SendArgsForCall(0)
	if strings.TrimSpace(out.Content) == "" {
		t.Fatal("expected non-empty stats reply")
	}
	if len(out.Buttons) != 2 || out.Buttons[0].Data != "/stats 24h" {
		t.Fatalf("expected 24h/7d window buttons, got %+v", out.Buttons)
	}
}

func TestHandleCommand_ConversationsList(t *testing.T) {
	cm, ch, store := newCommandTestManager(t)
	ctx := context.Background()

	groupKey := domain.FormatChannelSessionID("telegram", "42")
	currentID, oldID := uuid.NewString(), uuid.NewString()
	if err := store.SaveConversation(ctx, currentID, nil, storage.ConversationMetadata{Title: "Weather talk", MessageCount: 4}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConversation(ctx, oldID, nil, storage.ConversationMetadata{Title: "Trip planning", MessageCount: 9}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSessionGroup(ctx, groupKey, storage.SessionGroupEntry{CurrentSessionID: currentID, History: []string{oldID}}); err != nil {
		t.Fatal(err)
	}

	cm.handleCommand(ctx, domain.InboundMessage{ChannelName: "telegram", SenderID: "42", Content: "/conversations"}, "conversations", nil)

	if ch.SendCallCount() != 1 {
		t.Fatalf("expected 1 send, got %d", ch.SendCallCount())
	}
	_, out := ch.SendArgsForCall(0)
	if len(out.Buttons) != 2 {
		t.Fatalf("expected one button per conversation, got %+v", out.Buttons)
	}
	if out.Buttons[0].Data != "/conversations "+currentID || !strings.Contains(out.Buttons[0].Text, "Weather talk") {
		t.Fatalf("expected current conversation first, got %+v", out.Buttons[0])
	}
	if !strings.HasPrefix(out.Buttons[0].Text, "▸ ") {
		t.Fatalf("expected current marker on first button, got %q", out.Buttons[0].Text)
	}
	if out.Buttons[1].Data != "/conversations "+oldID || !strings.Contains(out.Buttons[1].Text, "Trip planning") {
		t.Fatalf("expected history conversation second, got %+v", out.Buttons[1])
	}
}

func TestHandleCommand_ConversationsSwitch(t *testing.T) {
	cm, ch, store := newCommandTestManager(t)
	ctx := context.Background()

	groupKey := domain.FormatChannelSessionID("telegram", "42")
	currentID, oldID := uuid.NewString(), uuid.NewString()
	tripEntries := []domain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Plan me a trip to Rome")}},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("Day 1: Colosseum and Forum.")}},
	}
	if err := store.SaveConversation(ctx, currentID, nil, storage.ConversationMetadata{Title: "Weather talk"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConversation(ctx, oldID, tripEntries, storage.ConversationMetadata{Title: "Trip planning"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutSessionGroup(ctx, groupKey, storage.SessionGroupEntry{CurrentSessionID: currentID, History: []string{oldID}}); err != nil {
		t.Fatal(err)
	}

	// Full UUID — the shape a button tap sends.
	cm.handleCommand(ctx, domain.InboundMessage{ChannelName: "telegram", SenderID: "42", Content: "/conversations " + oldID}, "conversations", []string{oldID})

	entry, _, _ := store.GetSessionGroup(ctx, groupKey)
	if entry.CurrentSessionID != oldID {
		t.Fatalf("expected switch to %s, got %q", oldID, entry.CurrentSessionID)
	}
	if len(entry.History) != 1 || entry.History[0] != currentID {
		t.Fatalf("expected old current in history without the target, got %v", entry.History)
	}
	_, out := ch.SendArgsForCall(ch.SendCallCount() - 1)
	if !strings.Contains(out.Content, "Trip planning") {
		t.Fatalf("expected switch confirmation with title, got %q", out.Content)
	}
	if !strings.Contains(out.Content, "You: Plan me a trip to Rome") || !strings.Contains(out.Content, "Agent: Day 1: Colosseum and Forum.") {
		t.Fatalf("expected last-exchange recap, got %q", out.Content)
	}

	// Unique prefix — the typed shape.
	cm.handleCommand(ctx, domain.InboundMessage{ChannelName: "telegram", SenderID: "42", Content: "/conversations " + currentID[:8]}, "conversations", []string{currentID[:8]})
	entry, _, _ = store.GetSessionGroup(ctx, groupKey)
	if entry.CurrentSessionID != currentID {
		t.Fatalf("expected prefix switch back to %s, got %q", currentID, entry.CurrentSessionID)
	}

	// Unknown prefix — no mutation.
	before := entry
	cm.handleCommand(ctx, domain.InboundMessage{ChannelName: "telegram", SenderID: "42", Content: "/conversations zzzz"}, "conversations", []string{"zzzz"})
	entry, _, _ = store.GetSessionGroup(ctx, groupKey)
	if entry.CurrentSessionID != before.CurrentSessionID {
		t.Fatalf("unknown prefix must not mutate the group, got %+v", entry)
	}
	_, out = ch.SendArgsForCall(ch.SendCallCount() - 1)
	if !strings.Contains(out.Content, "No conversation matches") {
		t.Fatalf("expected not-found reply, got %q", out.Content)
	}
}

func TestHandleCommand_Help(t *testing.T) {
	cm, ch, _ := newCommandTestManager(t)

	cm.handleCommand(context.Background(), domain.InboundMessage{ChannelName: "telegram", SenderID: "42", Content: "/help"}, "help", nil)

	if ch.SendCallCount() != 1 {
		t.Fatalf("expected 1 send, got %d", ch.SendCallCount())
	}
	_, out := ch.SendArgsForCall(0)
	if !strings.Contains(out.Content, "/clear") || !strings.Contains(out.Content, "/context") {
		t.Fatalf("expected help to list built-ins and TUI-only commands, got:\n%s", out.Content)
	}
}

func TestHandleCommand_TUIOnly(t *testing.T) {
	cm, ch, _ := newCommandTestManager(t)

	cm.handleCommand(context.Background(), domain.InboundMessage{ChannelName: "telegram", SenderID: "42", Content: "/context"}, "context", nil)

	if ch.SendCallCount() != 1 {
		t.Fatalf("expected 1 send, got %d", ch.SendCallCount())
	}
	_, out := ch.SendArgsForCall(0)
	if !strings.Contains(out.Content, "infer chat") {
		t.Fatalf("expected TUI-only reply, got %q", out.Content)
	}
}

func TestHandleCommand_CustomShortcut(t *testing.T) {
	cm, ch, _ := newCommandTestManager(t)
	cm.shortcutRegistry.Register(shortcuts.NewCustomShortcut(shortcuts.CustomShortcutConfig{
		Name:        "echo",
		Description: "echo test",
		Command:     "echo",
		Args:        []string{"hello from custom"},
	}, nil, nil, nil, nil))

	cm.handleCommand(context.Background(), domain.InboundMessage{ChannelName: "telegram", SenderID: "42", Content: "/echo"}, "echo", nil)

	if ch.SendCallCount() != 1 {
		t.Fatalf("expected 1 send, got %d", ch.SendCallCount())
	}
	_, out := ch.SendArgsForCall(0)
	if !strings.Contains(out.Content, "hello from custom") {
		t.Fatalf("expected command output forwarded, got %q", out.Content)
	}
}
