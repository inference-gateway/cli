package conversation

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	models "github.com/inference-gateway/cli/internal/platform/models"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// seedContextWindow registers a gateway-reported context window for the test's
// lifetime; without it token triggers gate off (unknown window).
func seedContextWindow(t *testing.T, model string, tokens int) {
	t.Helper()
	models.SetGatewayContextWindows(map[string]int{model: tokens})
	t.Cleanup(func() { models.SetGatewayContextWindows(nil) })
}

// optimizeTailStub is an OptimizeMessages stub standing in for the real
// optimizer: it replaces the conversation head with a summary message and
// keeps the last tail entries, or echoes the input unchanged when tail is 0
// (the failed-summary path).
func optimizeTailStub(tail int) func([]sdk.Message, string, bool) []sdk.Message {
	return func(messages []sdk.Message, _ string, _ bool) []sdk.Message {
		if tail <= 0 || tail >= len(messages) {
			return messages
		}
		out := make([]sdk.Message, 0, tail+1)
		out = append(out, sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent("--- Context Summary ---\n\nfake summary\n\n--- End Summary ---"),
		})
		for i := len(messages) - tail; i < len(messages); i++ {
			if i < 0 {
				continue
			}
			out = append(out, messages[i])
		}
		return out
	}
}

func newRolloverManagerForTest(t *testing.T, autoAt int, idleMin int) (*SessionRollover, *PersistentConversationRepository, *convmocks.FakeConversationOptimizer, storage.SessionGroupStorage, func()) {
	t.Helper()

	storageBackend, err := storage.NewSQLiteStorage(storage.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("create sqlite storage: %v", err)
	}
	repo := NewPersistentConversationRepository(nil, nil, storageBackend)

	cfg := &config.Config{}
	cfg.Compact.Enabled = true
	cfg.Compact.AutoAt = autoAt
	cfg.Compact.RolloverOnIdleMinutes = idleMin
	cfg.Compact.KeepFirstMessages = 2

	opt := &convmocks.FakeConversationOptimizer{}
	opt.OptimizeMessagesStub = optimizeTailStub(1)

	groupStore := storage.NewMemorySessionGroupStorage()
	mgr := NewSessionRollover(cfg, opt, repo, NewTokenizerService(DefaultTokenizerConfig()), groupStore)

	cleanup := func() {
		_ = repo.Close()
		_ = storageBackend.Close()
	}
	return mgr, repo, opt, groupStore, cleanup
}

func addUserMessage(t *testing.T, repo *PersistentConversationRepository, content string, when time.Time) {
	t.Helper()
	entry := convdomain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(content),
		},
		Time: when,
	}
	if err := repo.AddMessage(entry); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
}

func TestResolveSessionID_UUIDPassthrough(t *testing.T) {
	mgr, _, _, groupStore, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	literal := uuid.New().String()
	got, group, err := mgr.ResolveSessionID(literal)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != literal {
		t.Errorf("UUID should pass through unchanged: got %q want %q", got, literal)
	}
	if group != "" {
		t.Errorf("UUID input should produce empty group key, got %q", group)
	}

	all, err := groupStore.ListSessionGroups(context.Background())
	if err != nil {
		t.Fatalf("ListSessionGroups: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("UUID passthrough must not register any group entries; got %d", len(all))
	}
}

func TestResolveSessionID_GroupKeyMigration(t *testing.T) {
	mgr, _, _, groupStore, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	got, group, err := mgr.ResolveSessionID("channel-telegram-12345")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "channel-telegram-12345" {
		t.Errorf("first lookup should pass raw id through as initial session, got %q", got)
	}
	if group != "channel-telegram-12345" {
		t.Errorf("group key should be set, got %q", group)
	}

	entry, ok, err := groupStore.GetSessionGroup(context.Background(), "channel-telegram-12345")
	if err != nil {
		t.Fatalf("GetSessionGroup: %v", err)
	}
	if !ok {
		t.Fatalf("group should be registered after first lookup")
	}
	if entry.CurrentSessionID != "channel-telegram-12345" {
		t.Errorf("initial current_session_id should equal raw id, got %q", entry.CurrentSessionID)
	}
}

func TestResolveSessionID_GroupKeyAfterRollover(t *testing.T) {
	mgr, _, _, groupStore, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	rolledOverID := uuid.New().String()
	if err := groupStore.PutSessionGroup(context.Background(), "channel-telegram-12345", storage.SessionGroupEntry{
		CurrentSessionID: rolledOverID,
		History:          []string{"channel-telegram-12345"},
		LastRollover:     time.Now(),
		UpdatedAt:        time.Now(),
	}); err != nil {
		t.Fatalf("seed index: %v", err)
	}

	got, group, err := mgr.ResolveSessionID("channel-telegram-12345")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != rolledOverID {
		t.Errorf("group key should resolve to rolled-over UUID: got %q want %q", got, rolledOverID)
	}
	if group != "channel-telegram-12345" {
		t.Errorf("group key should be returned, got %q", group)
	}
}

// addBigMessages seeds n copies of a large user message so the entries-only
// token estimate exceeds any small-model threshold.
func addBigMessages(t *testing.T, repo *PersistentConversationRepository, n int) {
	t.Helper()
	bigContent := strings.Repeat("token ", 2000)
	for i := 0; i < n; i++ {
		addUserMessage(t, repo, bigContent, time.Now())
	}
}

func addTokenUsage(t *testing.T, repo *PersistentConversationRepository, model string, input, output, total int) {
	t.Helper()
	if err := repo.AddTokenUsage(model, input, output, total, 0, 0); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}
}

func TestShouldRollover(t *testing.T) {
	seedContextWindow(t, "moonshot/moonshot-v1-8k", 8192)
	tests := []struct {
		name       string
		autoAt     int
		idleMin    int
		compactOff bool
		seed       func(t *testing.T, repo *PersistentConversationRepository)
		model      string
		want       bool
	}{
		{
			name: "empty conversation does not trigger", autoAt: 80, idleMin: 30,
			model: "openai/gpt-4", want: false,
		},
		{
			name: "idle trigger fires past threshold", autoAt: 80, idleMin: 30,
			seed: func(t *testing.T, repo *PersistentConversationRepository) {
				addUserMessage(t, repo, "old message", time.Now().Add(-31*time.Minute))
			},
			model: "openai/gpt-4", want: true,
		},
		{
			name: "idle trigger does not fire under threshold", autoAt: 80, idleMin: 30,
			seed: func(t *testing.T, repo *PersistentConversationRepository) {
				addUserMessage(t, repo, "recent", time.Now().Add(-5*time.Minute))
			},
			model: "openai/gpt-4", want: false,
		},
		{
			name: "idle trigger disabled by zero threshold", autoAt: 80, idleMin: 0,
			seed: func(t *testing.T, repo *PersistentConversationRepository) {
				addUserMessage(t, repo, "old", time.Now().Add(-24*time.Hour))
			},
			model: "openai/gpt-4", want: false,
		},
		{
			name: "token trigger fires on large conversation", autoAt: 80, idleMin: 0,
			seed: func(t *testing.T, repo *PersistentConversationRepository) {
				addBigMessages(t, repo, 10)
			},
			model: "moonshot/moonshot-v1-8k", want: true,
		},
		{
			name: "token trigger fires from LastInputTokens despite small estimate", autoAt: 80, idleMin: 0,
			seed: func(t *testing.T, repo *PersistentConversationRepository) {
				addUserMessage(t, repo, "hi", time.Now())
				addTokenUsage(t, repo, "moonshot/moonshot-v1-8k", 7000, 100, 7100)
			},
			model: "moonshot/moonshot-v1-8k", want: true,
		},
		{
			name: "token trigger fires on single-turn spike despite stale LastInputTokens", autoAt: 80, idleMin: 0,
			seed: func(t *testing.T, repo *PersistentConversationRepository) {
				addBigMessages(t, repo, 10)
				addTokenUsage(t, repo, "moonshot/moonshot-v1-8k", 1000, 100, 1100)
			},
			model: "moonshot/moonshot-v1-8k", want: true,
		},
		{
			name: "token trigger ignores hidden entries the rollover would drop", autoAt: 80, idleMin: 0,
			seed: func(t *testing.T, repo *PersistentConversationRepository) {
				for range 10 {
					hidden := convdomain.ConversationEntry{
						Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(strings.Repeat("token ", 2000))},
						Time:    time.Now(),
						Hidden:  true,
					}
					if err := repo.AddMessage(hidden); err != nil {
						t.Fatalf("AddMessage: %v", err)
					}
				}
			},
			model: "moonshot/moonshot-v1-8k", want: false,
		},
		{
			name: "token trigger skipped for model with unknown context window", autoAt: 80, idleMin: 0,
			seed: func(t *testing.T, repo *PersistentConversationRepository) {
				addBigMessages(t, repo, 10)
				addTokenUsage(t, repo, "ollama_cloud/some-unlisted-model", 500000, 100, 500100)
			},
			model: "ollama_cloud/some-unlisted-model", want: false,
		},
		{
			name: "compact disabled turns off all triggers", autoAt: 80, idleMin: 30, compactOff: true,
			seed: func(t *testing.T, repo *PersistentConversationRepository) {
				addUserMessage(t, repo, "old", time.Now().Add(-31*time.Minute))
			},
			model: "openai/gpt-4", want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, tt.autoAt, tt.idleMin)
			defer cleanup()
			if tt.compactOff {
				mgr.cfg.Compact.Enabled = false
			}
			if tt.seed != nil {
				tt.seed(t, repo)
			}

			if got := mgr.ShouldRollover(tt.model); got != tt.want {
				t.Errorf("ShouldRollover(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestPerformRollover_CreatesNewSessionAndUpdatesIndex(t *testing.T) {
	mgr, repo, opt, groupStore, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	if _, _, err := mgr.ResolveSessionID("channel-telegram-12345"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := repo.StartNewConversation("Initial"); err != nil {
		t.Fatalf("start: %v", err)
	}
	originalID := repo.GetCurrentConversationID()

	for i := 0; i < 5; i++ {
		addUserMessage(t, repo, "msg", time.Now())
	}

	newID, err := mgr.PerformRollover(context.Background(), "openai/gpt-4", "channel-telegram-12345")
	if err != nil {
		t.Fatalf("PerformRollover: %v", err)
	}
	if newID == "" || newID == originalID {
		t.Errorf("new session id should differ from original; got %q (original %q)", newID, originalID)
	}
	if got := opt.OptimizeMessagesCallCount(); got != 1 {
		t.Errorf("optimizer should have been called once, got %d", got)
	}

	if got := repo.GetCurrentConversationID(); got != newID {
		t.Errorf("repo should now point at new session: got %q want %q", got, newID)
	}

	entry, ok, err := groupStore.GetSessionGroup(context.Background(), "channel-telegram-12345")
	if err != nil {
		t.Fatalf("GetSessionGroup: %v", err)
	}
	if !ok {
		t.Fatalf("group should still be registered after rollover")
	}
	if entry.CurrentSessionID != newID {
		t.Errorf("group current_session_id should be %q, got %q", newID, entry.CurrentSessionID)
	}
	if len(entry.History) == 0 || entry.History[len(entry.History)-1] != originalID {
		t.Errorf("group history should record previous id %q, got %v", originalID, entry.History)
	}
	if entry.LastRollover.IsZero() {
		t.Error("LastRollover should be set after rollover")
	}
}

func TestPerformRollover_ErrorWhenNoMessages(t *testing.T) {
	mgr, _, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	if _, err := mgr.PerformRollover(context.Background(), "openai/gpt-4", ""); err == nil {
		t.Error("expected error rolling over an empty conversation")
	}
}

func TestPutSessionGroup_OverwriteReplacesPriorEntry(t *testing.T) {
	_, _, _, groupStore, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	ctx := context.Background()
	if err := groupStore.PutSessionGroup(ctx, "a", storage.SessionGroupEntry{
		CurrentSessionID: "id-1", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := groupStore.PutSessionGroup(ctx, "a", storage.SessionGroupEntry{
		CurrentSessionID: "id-2", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("second put: %v", err)
	}
	if err := groupStore.PutSessionGroup(ctx, "b", storage.SessionGroupEntry{
		CurrentSessionID: "id-3", UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("third put: %v", err)
	}

	got, ok, err := groupStore.GetSessionGroup(ctx, "a")
	if err != nil || !ok {
		t.Fatalf("get a: %v ok=%v", err, ok)
	}
	if got.CurrentSessionID != "id-2" {
		t.Errorf("second put should overwrite first: got %q", got.CurrentSessionID)
	}
	if _, ok, _ := groupStore.GetSessionGroup(ctx, "b"); !ok {
		t.Errorf("expected b to be present")
	}
}

func TestConcurrentRolloversForDifferentGroups(t *testing.T) {
	mgr, _, _, groupStore, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	const groupCount = 8
	var wg sync.WaitGroup
	for i := 0; i < groupCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			groupKey := uuid.New().String()[:8] + "-not-a-uuid"
			if _, _, err := mgr.ResolveSessionID(groupKey); err != nil {
				t.Errorf("resolve: %v", err)
			}
		}(i)
	}
	wg.Wait()

	all, err := groupStore.ListSessionGroups(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != groupCount {
		t.Errorf("expected %d groups after concurrent registration, got %d", groupCount, len(all))
	}
}

func TestMaybeRollover_NilReceiverReturnsFalse(t *testing.T) {
	var mgr *SessionRollover
	newID, fired := mgr.MaybeRollover(context.Background(), "openai/gpt-4", "")
	if fired {
		t.Error("nil receiver must return fired=false")
	}
	if newID != "" {
		t.Errorf("nil receiver must return empty newID, got %q", newID)
	}
}

func TestMaybeRollover_GateClosedReturnsFalse(t *testing.T) {
	mgr, _, opt, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	newID, fired := mgr.MaybeRollover(context.Background(), "openai/gpt-4", "")
	if fired {
		t.Error("MaybeRollover with closed gate must return fired=false")
	}
	if newID != "" {
		t.Errorf("MaybeRollover with closed gate must return empty newID, got %q", newID)
	}
	if got := opt.OptimizeMessagesCallCount(); got != 0 {
		t.Errorf("optimizer must not be called when gate is closed; got %d", got)
	}
}

func TestMaybeRollover_FiresAndReturnsNewID(t *testing.T) {
	seedContextWindow(t, "moonshot/moonshot-v1-8k", 8192)
	mgr, repo, opt, groupStore, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	if _, _, err := mgr.ResolveSessionID("channel-test-group"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := repo.StartNewConversation("Initial"); err != nil {
		t.Fatalf("start: %v", err)
	}
	originalID := repo.GetCurrentConversationID()

	for _, content := range []string{"hi", "there", "again"} {
		addUserMessage(t, repo, content, time.Now())
	}
	if err := repo.AddTokenUsage("moonshot/moonshot-v1-8k", 7000, 100, 7100, 0, 0); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}

	newID, fired := mgr.MaybeRollover(context.Background(), "moonshot/moonshot-v1-8k", "channel-test-group")
	if !fired {
		t.Fatal("MaybeRollover should have fired with LastInputTokens above threshold")
	}
	if _, _, force := opt.OptimizeMessagesArgsForCall(0); force {
		t.Error("a token-triggered rollover must let the optimizer apply its own threshold and backoff (force=false)")
	}
	if newID == "" || newID == originalID {
		t.Errorf("MaybeRollover must return a new session id; got %q (original %q)", newID, originalID)
	}
	if got := opt.OptimizeMessagesCallCount(); got != 1 {
		t.Errorf("optimizer should have been called once via PerformRollover, got %d", got)
	}
	if got := repo.GetCurrentConversationID(); got != newID {
		t.Errorf("repo should point at new session: got %q want %q", got, newID)
	}
	entry, ok, err := groupStore.GetSessionGroup(context.Background(), "channel-test-group")
	if err != nil || !ok {
		t.Fatalf("GetSessionGroup: ok=%v err=%v", ok, err)
	}
	if entry.CurrentSessionID != newID {
		t.Errorf("group index must point at new id %q, got %q", newID, entry.CurrentSessionID)
	}
	if len(entry.History) == 0 || entry.History[len(entry.History)-1] != originalID {
		t.Errorf("group history must record previous id %q, got %v", originalID, entry.History)
	}
}

func TestMaybeRollover_PerformRolloverErrorReturnsFalse(t *testing.T) {
	seedContextWindow(t, "moonshot/moonshot-v1-8k", 8192)
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	if err := repo.StartNewConversation("Initial"); err != nil {
		t.Fatalf("start: %v", err)
	}
	originalID := repo.GetCurrentConversationID()

	hidden := convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hidden")},
		Time:    time.Now(),
		Hidden:  true,
	}
	if err := repo.AddMessage(hidden); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if err := repo.AddTokenUsage("moonshot/moonshot-v1-8k", 7000, 100, 7100, 0, 0); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}

	newID, fired := mgr.MaybeRollover(context.Background(), "moonshot/moonshot-v1-8k", "")
	if fired {
		t.Error("MaybeRollover must return fired=false when PerformRollover errors")
	}
	if newID != "" {
		t.Errorf("MaybeRollover must return empty newID on error, got %q", newID)
	}
	if got := repo.GetCurrentConversationID(); got != originalID {
		t.Errorf("repo must not be rolled over on PerformRollover error; got %q want %q", got, originalID)
	}
}

// TestMaybeRollover_NoReductionKeepsSession pins the failed-summary path: when
// the optimizer returns the conversation unchanged (failure, backoff, below its
// own threshold), a token-triggered rollover must not start a new session or
// touch the group index.
func TestMaybeRollover_NoReductionKeepsSession(t *testing.T) {
	seedContextWindow(t, "moonshot/moonshot-v1-8k", 8192)
	mgr, repo, opt, groupStore, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()
	opt.OptimizeMessagesStub = optimizeTailStub(0)

	if _, _, err := mgr.ResolveSessionID("channel-test-group"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := repo.StartNewConversation("Initial"); err != nil {
		t.Fatalf("start: %v", err)
	}
	originalID := repo.GetCurrentConversationID()
	addBigMessages(t, repo, 10)

	for range 3 {
		if _, fired := mgr.MaybeRollover(context.Background(), "moonshot/moonshot-v1-8k", "channel-test-group"); fired {
			t.Fatal("MaybeRollover must not fire when the optimizer returned no reduction")
		}
	}
	if got := repo.GetCurrentConversationID(); got != originalID {
		t.Errorf("session must stay %q without a summary, got %q", originalID, got)
	}
	if n := len(repo.GetMessages()); n != 10 {
		t.Errorf("conversation must be left as-is, got %d messages", n)
	}
	entry, ok, err := groupStore.GetSessionGroup(context.Background(), "channel-test-group")
	if err != nil || !ok {
		t.Fatalf("GetSessionGroup: ok=%v err=%v", ok, err)
	}
	if len(entry.History) != 0 {
		t.Errorf("group history must not record a rollover, got %v", entry.History)
	}
}

// TestMaybeRollover_IdleTriggerForces pins that the idle trigger still compacts
// with force, since it fires regardless of the optimizer's token threshold.
func TestMaybeRollover_IdleTriggerForces(t *testing.T) {
	mgr, repo, opt, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	if err := repo.StartNewConversation("Initial"); err != nil {
		t.Fatalf("start: %v", err)
	}
	for _, content := range []string{"old", "older", "oldest"} {
		addUserMessage(t, repo, content, time.Now().Add(-31*time.Minute))
	}

	if _, fired := mgr.MaybeRollover(context.Background(), "openai/gpt-4", ""); !fired {
		t.Fatal("idle trigger should have rolled over")
	}
	if _, _, force := opt.OptimizeMessagesArgsForCall(0); !force {
		t.Error("an idle-triggered rollover must compact with force=true")
	}
}
