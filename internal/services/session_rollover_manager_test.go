package services

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	uuid "github.com/google/uuid"
	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
)

// fakeOptimizer is a minimal in-test stand-in for ConversationOptimizer that
// reports a configurable summary count and records every call.
type fakeOptimizer struct {
	mu          sync.Mutex
	calls       int
	returnCount int // how many messages to return; 0 → return input unchanged
}

func (f *fakeOptimizer) OptimizeMessages(messages []sdk.Message, model string, force bool) []sdk.Message {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.returnCount <= 0 || f.returnCount >= len(messages) {
		return messages
	}
	out := make([]sdk.Message, 0, f.returnCount)
	out = append(out, sdk.Message{
		Role:    sdk.Assistant,
		Content: sdk.NewMessageContent("--- Context Summary ---\n\nfake summary\n\n--- End Summary ---"),
	})
	for i := len(messages) - (f.returnCount - 1); i < len(messages); i++ {
		if i < 0 {
			continue
		}
		out = append(out, messages[i])
	}
	return out
}

func newRolloverManagerForTest(t *testing.T, autoAt int, idleMin int) (*SessionRolloverManager, *PersistentConversationRepository, *fakeOptimizer, storage.SessionGroupStorage, func()) {
	t.Helper()

	storageBackend, err := storage.NewSQLiteStorage(storage.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatalf("create sqlite storage: %v", err)
	}
	repo := NewPersistentConversationRepository(&ToolFormatterService{}, nil, storageBackend)

	cfg := &config.Config{}
	cfg.Compact.Enabled = true
	cfg.Compact.AutoAt = autoAt
	cfg.Compact.RolloverOnIdleMinutes = idleMin
	cfg.Compact.KeepFirstMessages = 2

	opt := &fakeOptimizer{returnCount: 2}

	groupStore := storage.NewMemorySessionGroupStorage()
	mgr := NewSessionRolloverManager(cfg, opt, repo, NewTokenizerService(DefaultTokenizerConfig()), groupStore)

	cleanup := func() {
		_ = repo.Close()
		_ = storageBackend.Close()
	}
	return mgr, repo, opt, groupStore, cleanup
}

func addUserMessage(t *testing.T, repo *PersistentConversationRepository, content string, when time.Time) {
	t.Helper()
	entry := domain.ConversationEntry{
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

	// Pre-populate the index as if a rollover has already happened.
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

func TestShouldRollover_EmptyConversation(t *testing.T) {
	mgr, _, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	if mgr.ShouldRollover("openai/gpt-4") {
		t.Error("empty conversation should not trigger rollover")
	}
}

func TestShouldRollover_IdleTriggerFires(t *testing.T) {
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	addUserMessage(t, repo, "old message", time.Now().Add(-31*time.Minute))

	if !mgr.ShouldRollover("openai/gpt-4") {
		t.Error("31 min old message should trigger idle rollover (threshold=30)")
	}
}

func TestShouldRollover_IdleTriggerDoesNotFireUnderThreshold(t *testing.T) {
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	addUserMessage(t, repo, "recent", time.Now().Add(-5*time.Minute))

	if mgr.ShouldRollover("openai/gpt-4") {
		t.Error("5 min old message should not trigger idle rollover")
	}
}

func TestShouldRollover_IdleDisabledByZero(t *testing.T) {
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	addUserMessage(t, repo, "old", time.Now().Add(-24*time.Hour))

	if mgr.ShouldRollover("openai/gpt-4") {
		t.Error("idle threshold 0 should disable the trigger entirely")
	}
}

func TestShouldRollover_TokenTriggerFires(t *testing.T) {
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	bigContent := strings.Repeat("token ", 2000)
	for i := 0; i < 10; i++ {
		addUserMessage(t, repo, bigContent, time.Now())
	}

	if !mgr.ShouldRollover("moonshot/moonshot-v1-8k") {
		t.Error("large conversation should trigger token rollover against known context window")
	}
}

func TestShouldRollover_TokenTriggerFiresFromLastInputTokens(t *testing.T) {
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	// One short message - entries-only estimate is far below the threshold.
	addUserMessage(t, repo, "hi", time.Now())

	// Simulate the gateway reporting a large prompt_tokens value (system
	// prompt + tool defs + history). Threshold for moonshot-v1-8k is
	// 8192*80/100=6553 tokens.
	if err := repo.AddTokenUsage("moonshot/moonshot-v1-8k", 7000, 100, 7100); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}

	if !mgr.ShouldRollover("moonshot/moonshot-v1-8k") {
		t.Error("LastInputTokens above threshold should trigger token rollover even when entries-only estimate is small")
	}
}

func TestShouldRollover_TokenTriggerDoesNotFireWhenLastInputBelowThreshold(t *testing.T) {
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	// Large entries that *would* trip the entries-only fallback…
	bigContent := strings.Repeat("token ", 2000)
	for i := 0; i < 10; i++ {
		addUserMessage(t, repo, bigContent, time.Now())
	}

	// …but the gateway-reported count is well below the threshold. The
	// gateway count must win - its number includes provider-specific
	// reformatting and is what `/context` shows.
	if err := repo.AddTokenUsage("moonshot/moonshot-v1-8k", 1000, 100, 1100); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}

	if mgr.ShouldRollover("moonshot/moonshot-v1-8k") {
		t.Error("LastInputTokens below threshold should not trigger rollover, even if entries-only estimate is large")
	}
}

// TestShouldRollover_TokenTriggerSkippedForUnknownModel verifies that a model
// with no configured context window never trips the token trigger, no matter
// how large the conversation or the gateway-reported token count. Otherwise the
// session would roll over against the default fallback window every few messages
// (this was the minimax-m3 bug, before that model was added to the registry).
// The idle trigger is disabled here (idleMin=0) to isolate the token path.
func TestShouldRollover_TokenTriggerSkippedForUnknownModel(t *testing.T) {
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	bigContent := strings.Repeat("token ", 2000)
	for i := 0; i < 10; i++ {
		addUserMessage(t, repo, bigContent, time.Now())
	}
	// A gateway-reported count far above any default-window threshold.
	if err := repo.AddTokenUsage("ollama_cloud/some-unlisted-model", 500000, 100, 500100); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}

	if mgr.ShouldRollover("ollama_cloud/some-unlisted-model") {
		t.Error("model with no configured context window must not trigger token rollover")
	}
}

func TestShouldRollover_DisabledWhenCompactOff(t *testing.T) {
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()
	mgr.cfg.Compact.Enabled = false

	addUserMessage(t, repo, "old", time.Now().Add(-31*time.Minute))

	if mgr.ShouldRollover("openai/gpt-4") {
		t.Error("compact.enabled=false should disable all rollover triggers")
	}
}

func TestPerformRollover_CreatesNewSessionAndUpdatesIndex(t *testing.T) {
	mgr, repo, opt, groupStore, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	// Seed the group index so PerformRollover has a known initial state.
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
	if opt.calls != 1 {
		t.Errorf("optimizer should have been called once, got %d", opt.calls)
	}

	if got := repo.GetCurrentConversationID(); got != newID {
		t.Errorf("repo should now point at new session: got %q want %q", got, newID)
	}

	// Index should now reflect the rollover.
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
	var mgr *SessionRolloverManager
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
	if opt.calls != 0 {
		t.Errorf("optimizer must not be called when gate is closed; got %d", opt.calls)
	}
}

func TestMaybeRollover_FiresAndReturnsNewID(t *testing.T) {
	mgr, repo, opt, groupStore, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	if _, _, err := mgr.ResolveSessionID("channel-test-group"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := repo.StartNewConversation("Initial"); err != nil {
		t.Fatalf("start: %v", err)
	}
	originalID := repo.GetCurrentConversationID()

	addUserMessage(t, repo, "hi", time.Now())
	if err := repo.AddTokenUsage("moonshot/moonshot-v1-8k", 7000, 100, 7100); err != nil {
		t.Fatalf("AddTokenUsage: %v", err)
	}

	newID, fired := mgr.MaybeRollover(context.Background(), "moonshot/moonshot-v1-8k", "channel-test-group")
	if !fired {
		t.Fatal("MaybeRollover should have fired with LastInputTokens above threshold")
	}
	if newID == "" || newID == originalID {
		t.Errorf("MaybeRollover must return a new session id; got %q (original %q)", newID, originalID)
	}
	if opt.calls != 1 {
		t.Errorf("optimizer should have been called once via PerformRollover, got %d", opt.calls)
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
	mgr, repo, _, _, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	if err := repo.StartNewConversation("Initial"); err != nil {
		t.Fatalf("start: %v", err)
	}
	originalID := repo.GetCurrentConversationID()

	hidden := domain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hidden")},
		Time:    time.Now(),
		Hidden:  true,
	}
	if err := repo.AddMessage(hidden); err != nil {
		t.Fatalf("AddMessage: %v", err)
	}
	if err := repo.AddTokenUsage("moonshot/moonshot-v1-8k", 7000, 100, 7100); err != nil {
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
