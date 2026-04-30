package services

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	uuid "github.com/google/uuid"
	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	logger "github.com/inference-gateway/cli/internal/logger"
	models "github.com/inference-gateway/cli/internal/models"
)

// SessionRolloverManager decides when to roll over a long-running conversation
// into a new file (matching the chat-mode `/compact` behavior) and exposes the
// machinery to perform that rollover. It also resolves "group key" inputs from
// the channel manager (e.g. `channel-telegram-XYZ`) to the current session UUID
// via the configured SessionGroupStorage backend, so callers like the channel
// manager can keep using a stable, deterministic identifier without worrying
// about which physical session it points at right now.
type SessionRolloverManager struct {
	cfg        *config.Config
	optimizer  domain.ConversationOptimizer
	repo       *PersistentConversationRepository
	tokenizer  *TokenizerService
	groupStore storage.SessionGroupStorage
	indexMutex sync.Mutex
}

// NewSessionRolloverManager constructs a manager. The optimizer is required for
// PerformRollover to work; if it's nil, ShouldRollover always returns false and
// PerformRollover returns an error. This mirrors how the chat-mode /compact
// shortcut behaves when the optimizer is disabled. groupStore is required for
// non-UUID session-id resolution; if it is nil, group-keyed lookups will fall
// back to passing the raw id through.
func NewSessionRolloverManager(
	cfg *config.Config,
	optimizer domain.ConversationOptimizer,
	repo *PersistentConversationRepository,
	tokenizer *TokenizerService,
	groupStore storage.SessionGroupStorage,
) *SessionRolloverManager {
	if tokenizer == nil {
		tokenizer = NewTokenizerService(DefaultTokenizerConfig())
	}

	return &SessionRolloverManager{
		cfg:        cfg,
		optimizer:  optimizer,
		repo:       repo,
		tokenizer:  tokenizer,
		groupStore: groupStore,
	}
}

// ResolveSessionID maps a raw --session-id value to the conversation ID that
// should actually be loaded.
//
//   - If rawID parses as a UUID, it is treated as a literal session ID and
//     returned unchanged with an empty groupKey.
//   - Otherwise it is treated as a group key. The configured SessionGroupStorage
//     is consulted: if the group exists, its current_session_id is returned; if
//     it does not, the group is registered with rawID as its initial
//     current_session_id (this is the migration path for existing channel JSONL
//     files named after the deterministic group key).
//
// Returns (sessionID, groupKey, error). On any error reading/writing the
// store, the function logs a warning and falls back to passing rawID through
// unchanged so the agent can still run.
func (m *SessionRolloverManager) ResolveSessionID(rawID string) (string, string, error) {
	if rawID == "" {
		return "", "", nil
	}
	if _, err := uuid.Parse(rawID); err == nil {
		return rawID, "", nil
	}
	if m.groupStore == nil {
		logger.Warn("session group storage is not configured, using raw id", "raw_id", rawID)
		return rawID, rawID, nil
	}

	groupKey := rawID
	ctx := context.Background()

	m.indexMutex.Lock()
	defer m.indexMutex.Unlock()

	entry, found, err := m.groupStore.GetSessionGroup(ctx, groupKey)
	if err != nil {
		logger.Warn("failed to load session group, using raw id", "error", err, "raw_id", rawID)
		return rawID, groupKey, nil
	}

	if found && entry.CurrentSessionID != "" {
		return entry.CurrentSessionID, groupKey, nil
	}

	newEntry := storage.SessionGroupEntry{
		CurrentSessionID: rawID,
		UpdatedAt:        time.Now(),
	}
	if err := m.groupStore.PutSessionGroup(ctx, groupKey, newEntry); err != nil {
		logger.Warn("failed to register session group, using raw id", "error", err, "group", groupKey)
	}

	return rawID, groupKey, nil
}

// ShouldRollover checks the currently loaded conversation in the repo against
// both rollover triggers (idle and token threshold) and returns true if either
// fires. Returns false on a fresh/empty conversation, when the optimizer is
// disabled, or when compact.enabled=false.
func (m *SessionRolloverManager) ShouldRollover(model string) bool {
	if m.optimizer == nil || m.repo == nil || !m.cfg.Compact.Enabled {
		return false
	}

	entries := m.repo.GetMessages()
	if len(entries) == 0 {
		return false
	}

	if m.idleTriggerFires(entries) {
		logger.Info("session rollover triggered by idle threshold",
			"idle_minutes", m.cfg.Compact.RolloverOnIdleMinutes,
			"messages", len(entries))
		return true
	}

	if m.tokenTriggerFires(entries, model) {
		logger.Info("session rollover triggered by token threshold",
			"auto_at_percent", m.cfg.Compact.AutoAt,
			"messages", len(entries))
		return true
	}

	return false
}

// idleTriggerFires reports whether the most recent message is older than the
// configured rollover_on_idle_minutes. A value of 0 disables the check.
func (m *SessionRolloverManager) idleTriggerFires(entries []domain.ConversationEntry) bool {
	mins := m.cfg.Compact.RolloverOnIdleMinutes
	if mins <= 0 {
		return false
	}

	var lastTime time.Time
	for i := len(entries) - 1; i >= 0; i-- {
		if !entries[i].Time.IsZero() {
			lastTime = entries[i].Time
			break
		}
	}
	if lastTime.IsZero() {
		return false
	}

	return time.Since(lastTime) >= time.Duration(mins)*time.Minute
}

// tokenTriggerFires reports whether the conversation's estimated token count
// crosses compact.auto_at percent of the model's context window.
//
// Prefers the gateway-reported LastInputTokens from session stats: that value
// is the authoritative count of what was actually sent (including system
// prompt and tool definitions) and is also what `/context` displays, so the
// trigger and the UI stay in lock-step. Falls back to the entries-only
// estimate before the first round-trip when LastInputTokens is still zero.
func (m *SessionRolloverManager) tokenTriggerFires(entries []domain.ConversationEntry, model string) bool {
	autoAt := m.cfg.Compact.AutoAt
	if autoAt <= 0 || autoAt > 100 {
		autoAt = 80
	}

	contextWindow := models.EstimateContextWindow(model)
	if contextWindow == 0 {
		contextWindow = 30000
	}
	threshold := (contextWindow * autoAt) / 100

	if stats := m.repo.GetSessionTokens(); stats.LastInputTokens > 0 {
		return stats.LastInputTokens >= threshold
	}

	msgs := make([]sdk.Message, 0, len(entries))
	for _, e := range entries {
		msgs = append(msgs, e.Message)
	}
	return m.tokenizer.EstimateMessagesTokens(msgs) >= threshold
}

// PerformRollover runs the optimizer with force=true to produce a summary,
// calls StartNewConversation on the repo to begin a fresh conversation file,
// re-adds the summarized messages, and (if groupKey != "") updates the
// configured SessionGroupStorage to point the group at the new session ID.
//
// This mirrors performCompactAsync in chat_shortcut_handler.go:510-615 — same
// optimizer call, same StartNewConversation call, same AddMessage loop.
//
// Returns the new session UUID on success.
func (m *SessionRolloverManager) PerformRollover(ctx context.Context, model, groupKey string) (string, error) {
	if m.optimizer == nil {
		return "", errors.New("conversation optimizer is not enabled")
	}
	if m.repo == nil {
		return "", errors.New("conversation repository is not configured")
	}

	entries := m.repo.GetMessages()
	if len(entries) == 0 {
		return "", errors.New("no messages to rollover")
	}

	originalTitle := m.repo.GetCurrentConversationTitle()
	originalID := m.repo.GetCurrentConversationID()

	messages := make([]sdk.Message, 0, len(entries))
	for _, entry := range entries {
		if entry.Hidden {
			continue
		}
		messages = append(messages, entry.Message)
	}
	if len(messages) == 0 {
		return "", errors.New("no visible messages to rollover")
	}

	logger.Info("performing session rollover",
		"original_id", originalID,
		"group_key", groupKey,
		"message_count", len(messages),
		"model", model)

	optimized := m.optimizer.OptimizeMessages(messages, model, true)
	if len(optimized) >= len(messages) {
		// Optimizer decided no compaction was useful (e.g. very short
		// conversation that hit the idle trigger). Fall back to passing the
		// existing messages through to the new session unchanged so the
		// continuity is preserved.
		logger.Debug("optimizer returned no reduction, copying messages as-is")
	}

	newTitle := fmt.Sprintf("Continued from %s", originalTitle)
	if err := m.repo.StartNewConversation(newTitle); err != nil {
		return "", fmt.Errorf("failed to start new conversation: %w", err)
	}

	newID := m.repo.GetCurrentConversationID()

	for _, msg := range optimized {
		entry := domain.ConversationEntry{
			Message: msg,
			Model:   model,
			Time:    time.Now(),
		}
		if err := m.repo.AddMessage(entry); err != nil {
			logger.Error("failed to add summarized message to new session", "error", err)
		}
	}

	if groupKey != "" {
		if err := m.updateGroupIndex(ctx, groupKey, newID, originalID); err != nil {
			logger.Warn("failed to update session groups index", "error", err, "group", groupKey)
		}
	}

	logger.Info("session rollover complete",
		"new_id", newID,
		"original_id", originalID,
		"summarized_messages", len(optimized))

	return newID, nil
}

// updateGroupIndex sets the group's current_session_id to newID and appends
// the previous ID to the history.
func (m *SessionRolloverManager) updateGroupIndex(ctx context.Context, groupKey, newID, previousID string) error {
	if m.groupStore == nil {
		return errors.New("session group storage is not configured")
	}

	m.indexMutex.Lock()
	defer m.indexMutex.Unlock()

	entry, _, err := m.groupStore.GetSessionGroup(ctx, groupKey)
	if err != nil {
		return err
	}

	if previousID != "" && previousID != newID {
		entry.History = append(entry.History, previousID)
	}
	entry.CurrentSessionID = newID
	entry.LastRollover = time.Now()
	entry.UpdatedAt = time.Now()

	return m.groupStore.PutSessionGroup(ctx, groupKey, entry)
}
