package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func newRolloverManagerForTest(t *testing.T, autoAt int, idleMin int) (*SessionRolloverManager, *PersistentConversationRepository, *fakeOptimizer, func()) {
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

	tmpDir := t.TempDir()
	mgr := NewSessionRolloverManager(cfg, opt, repo, NewTokenizerService(DefaultTokenizerConfig()))
	mgr.SetIndexPath(filepath.Join(tmpDir, "session_groups.json"))

	cleanup := func() {
		_ = repo.Close()
		_ = storageBackend.Close()
	}
	return mgr, repo, opt, cleanup
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
	mgr, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
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

	if _, err := os.Stat(mgr.indexPath); err == nil {
		t.Errorf("UUID passthrough must not create index file")
	}
}

func TestResolveSessionID_GroupKeyMigration(t *testing.T) {
	mgr, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
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

	data, err := os.ReadFile(mgr.indexPath)
	if err != nil {
		t.Fatalf("expected index file to be created: %v", err)
	}
	var idx sessionGroupIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parse index: %v", err)
	}
	entry, ok := idx.Groups["channel-telegram-12345"]
	if !ok {
		t.Fatalf("group should be registered after first lookup")
	}
	if entry.CurrentSessionID != "channel-telegram-12345" {
		t.Errorf("initial current_session_id should equal raw id, got %q", entry.CurrentSessionID)
	}
}

func TestResolveSessionID_GroupKeyAfterRollover(t *testing.T) {
	mgr, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	// Pre-populate the index as if a rollover has already happened.
	rolledOverID := uuid.New().String()
	mgr.indexMutex.Lock()
	idx := &sessionGroupIndex{Groups: map[string]SessionGroupEntry{
		"channel-telegram-12345": {
			CurrentSessionID: rolledOverID,
			History:          []string{"channel-telegram-12345"},
			LastRollover:     time.Now(),
			UpdatedAt:        time.Now(),
		},
	}}
	if err := mgr.saveIndexAtomicLocked(idx); err != nil {
		mgr.indexMutex.Unlock()
		t.Fatalf("seed index: %v", err)
	}
	mgr.indexMutex.Unlock()

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
	mgr, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	if mgr.ShouldRollover("openai/gpt-4") {
		t.Error("empty conversation should not trigger rollover")
	}
}

func TestShouldRollover_IdleTriggerFires(t *testing.T) {
	mgr, repo, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	addUserMessage(t, repo, "old message", time.Now().Add(-31*time.Minute))

	if !mgr.ShouldRollover("openai/gpt-4") {
		t.Error("31 min old message should trigger idle rollover (threshold=30)")
	}
}

func TestShouldRollover_IdleTriggerDoesNotFireUnderThreshold(t *testing.T) {
	mgr, repo, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	addUserMessage(t, repo, "recent", time.Now().Add(-5*time.Minute))

	if mgr.ShouldRollover("openai/gpt-4") {
		t.Error("5 min old message should not trigger idle rollover")
	}
}

func TestShouldRollover_IdleDisabledByZero(t *testing.T) {
	mgr, repo, _, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	addUserMessage(t, repo, "old", time.Now().Add(-24*time.Hour))

	if mgr.ShouldRollover("openai/gpt-4") {
		t.Error("idle threshold 0 should disable the trigger entirely")
	}
}

func TestShouldRollover_TokenTriggerFires(t *testing.T) {
	mgr, repo, _, cleanup := newRolloverManagerForTest(t, 80, 0)
	defer cleanup()

	// Use a model with a tiny context window so we can blow past 80% with a
	// few messages. Default fallback is 8192 tokens; 80% = ~6553 tokens.
	// Each large message ≈ 1000 tokens via the tokenizer's char-based estimate.
	bigContent := strings.Repeat("token ", 2000) // ~2000 words ≈ many tokens
	for i := 0; i < 10; i++ {
		addUserMessage(t, repo, bigContent, time.Now())
	}

	if !mgr.ShouldRollover("unknown-tiny-model") {
		t.Error("large conversation should trigger token rollover against fallback context window")
	}
}

func TestShouldRollover_DisabledWhenCompactOff(t *testing.T) {
	mgr, repo, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()
	mgr.cfg.Compact.Enabled = false

	addUserMessage(t, repo, "old", time.Now().Add(-31*time.Minute))

	if mgr.ShouldRollover("openai/gpt-4") {
		t.Error("compact.enabled=false should disable all rollover triggers")
	}
}

func TestPerformRollover_CreatesNewSessionAndUpdatesIndex(t *testing.T) {
	mgr, repo, opt, cleanup := newRolloverManagerForTest(t, 80, 30)
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
	mgr.indexMutex.Lock()
	idx, err := mgr.loadIndexLocked()
	mgr.indexMutex.Unlock()
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	entry := idx.Groups["channel-telegram-12345"]
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
	mgr, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	if _, err := mgr.PerformRollover(context.Background(), "openai/gpt-4", ""); err == nil {
		t.Error("expected error rolling over an empty conversation")
	}
}

func TestSaveIndexAtomicLocked_OverwriteIsAtomic(t *testing.T) {
	mgr, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
	defer cleanup()

	mgr.indexMutex.Lock()
	idx1 := &sessionGroupIndex{Groups: map[string]SessionGroupEntry{
		"a": {CurrentSessionID: "id-1", UpdatedAt: time.Now()},
	}}
	if err := mgr.saveIndexAtomicLocked(idx1); err != nil {
		mgr.indexMutex.Unlock()
		t.Fatalf("first save: %v", err)
	}

	idx2 := &sessionGroupIndex{Groups: map[string]SessionGroupEntry{
		"a": {CurrentSessionID: "id-2", UpdatedAt: time.Now()},
		"b": {CurrentSessionID: "id-3", UpdatedAt: time.Now()},
	}}
	if err := mgr.saveIndexAtomicLocked(idx2); err != nil {
		mgr.indexMutex.Unlock()
		t.Fatalf("second save: %v", err)
	}
	mgr.indexMutex.Unlock()

	mgr.indexMutex.Lock()
	loaded, err := mgr.loadIndexLocked()
	mgr.indexMutex.Unlock()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Groups["a"].CurrentSessionID != "id-2" {
		t.Errorf("second save should overwrite first: got %q", loaded.Groups["a"].CurrentSessionID)
	}
	if _, ok := loaded.Groups["b"]; !ok {
		t.Errorf("second save should add b")
	}

	// No leftover .tmp files in the index dir.
	dir := filepath.Dir(mgr.indexPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), filepath.Base(mgr.indexPath)+".tmp-") {
			t.Errorf("temp file leaked: %s", e.Name())
		}
	}
}

func TestConcurrentRolloversForDifferentGroups(t *testing.T) {
	mgr, _, _, cleanup := newRolloverManagerForTest(t, 80, 30)
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

	mgr.indexMutex.Lock()
	idx, err := mgr.loadIndexLocked()
	mgr.indexMutex.Unlock()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(idx.Groups) != groupCount {
		t.Errorf("expected %d groups after concurrent registration, got %d", groupCount, len(idx.Groups))
	}
}
