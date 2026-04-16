package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	uuid "github.com/google/uuid"
	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	models "github.com/inference-gateway/cli/internal/models"
)

// sessionGroupsFileName is the on-disk index that maps a "group key" (any
// non-UUID identifier such as "channel-telegram-6312551834") to the current
// session UUID for that group. The file lives next to the conversations
// directory at <project>/.infer/session_groups.json.
const sessionGroupsFileName = "session_groups.json"

// SessionGroupEntry tracks the active session for a given group key plus a
// rollover history so old conversations can still be looked up via
// `infer conversations list`.
type SessionGroupEntry struct {
	CurrentSessionID string    `json:"current_session_id"`
	History          []string  `json:"history,omitempty"`
	LastRollover     time.Time `json:"last_rollover,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// sessionGroupIndex is the on-disk schema for session_groups.json.
type sessionGroupIndex struct {
	Groups map[string]SessionGroupEntry `json:"groups"`
}

// SessionRolloverManager decides when to roll over a long-running conversation
// into a new file (matching the chat-mode `/compact` behavior) and exposes the
// machinery to perform that rollover. It also resolves "group key" inputs from
// the channel manager (e.g. `channel-telegram-XYZ`) to the current session UUID
// via a small JSON index, so callers like the channel manager can keep using a
// stable, deterministic identifier without worrying about which physical
// session it points at right now.
type SessionRolloverManager struct {
	cfg        *config.Config
	optimizer  domain.ConversationOptimizer
	repo       *PersistentConversationRepository
	tokenizer  *TokenizerService
	indexPath  string
	indexMutex sync.Mutex
}

// NewSessionRolloverManager constructs a manager. The optimizer is required for
// PerformRollover to work; if it's nil, ShouldRollover always returns false and
// PerformRollover returns an error. This mirrors how the chat-mode /compact
// shortcut behaves when the optimizer is disabled.
func NewSessionRolloverManager(
	cfg *config.Config,
	optimizer domain.ConversationOptimizer,
	repo *PersistentConversationRepository,
	tokenizer *TokenizerService,
) *SessionRolloverManager {
	indexPath := filepath.Join(config.ConfigDirName, sessionGroupsFileName)
	if abs, err := filepath.Abs(indexPath); err == nil {
		indexPath = abs
	}

	if tokenizer == nil {
		tokenizer = NewTokenizerService(DefaultTokenizerConfig())
	}

	return &SessionRolloverManager{
		cfg:       cfg,
		optimizer: optimizer,
		repo:      repo,
		tokenizer: tokenizer,
		indexPath: indexPath,
	}
}

// SetIndexPath overrides the index file location. Used by tests.
func (m *SessionRolloverManager) SetIndexPath(path string) {
	m.indexMutex.Lock()
	defer m.indexMutex.Unlock()
	m.indexPath = path
}

// ResolveSessionID maps a raw --session-id value to the conversation ID that
// should actually be loaded.
//
//   - If rawID parses as a UUID, it is treated as a literal session ID and
//     returned unchanged with an empty groupKey.
//   - Otherwise it is treated as a group key. The index file is consulted: if
//     the group exists, its current_session_id is returned; if it does not,
//     the group is registered with rawID as its initial current_session_id
//     (this is the migration path for existing channel JSONL files named
//     after the deterministic group key).
//
// Returns (sessionID, groupKey, error). On any error reading/writing the
// index, the function logs a warning and falls back to passing rawID through
// unchanged so the agent can still run.
func (m *SessionRolloverManager) ResolveSessionID(rawID string) (string, string, error) {
	if rawID == "" {
		return "", "", nil
	}
	if _, err := uuid.Parse(rawID); err == nil {
		return rawID, "", nil
	}

	groupKey := rawID

	m.indexMutex.Lock()
	defer m.indexMutex.Unlock()

	idx, err := m.loadIndexLocked()
	if err != nil {
		logger.Warn("failed to load session groups index, using raw id", "error", err, "raw_id", rawID)
		return rawID, groupKey, nil
	}

	if entry, ok := idx.Groups[groupKey]; ok && entry.CurrentSessionID != "" {
		return entry.CurrentSessionID, groupKey, nil
	}

	idx.Groups[groupKey] = SessionGroupEntry{
		CurrentSessionID: rawID,
		UpdatedAt:        time.Now(),
	}
	if err := m.saveIndexAtomicLocked(idx); err != nil {
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
// crosses compact.auto_at percent of the model's context window. Mirrors the
// in-place check in conversation_optimizer.go:80-91.
func (m *SessionRolloverManager) tokenTriggerFires(entries []domain.ConversationEntry, model string) bool {
	autoAt := m.cfg.Compact.AutoAt
	if autoAt <= 0 || autoAt > 100 {
		autoAt = 80
	}

	msgs := make([]sdk.Message, 0, len(entries))
	for _, e := range entries {
		msgs = append(msgs, e.Message)
	}

	currentTokens := m.tokenizer.EstimateMessagesTokens(msgs)
	contextWindow := models.EstimateContextWindow(model)
	if contextWindow == 0 {
		contextWindow = 30000
	}

	threshold := (contextWindow * autoAt) / 100
	return currentTokens >= threshold
}

// PerformRollover runs the optimizer with force=true to produce a summary,
// calls StartNewConversation on the repo to begin a fresh conversation file,
// re-adds the summarized messages, and (if groupKey != "") updates the index
// to point the group at the new session ID.
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
		if err := m.updateGroupIndex(groupKey, newID, originalID); err != nil {
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
// the previous ID to the history. Acquires the index mutex.
func (m *SessionRolloverManager) updateGroupIndex(groupKey, newID, previousID string) error {
	m.indexMutex.Lock()
	defer m.indexMutex.Unlock()

	idx, err := m.loadIndexLocked()
	if err != nil {
		return err
	}

	entry := idx.Groups[groupKey]
	if previousID != "" && previousID != newID {
		entry.History = append(entry.History, previousID)
	}
	entry.CurrentSessionID = newID
	entry.LastRollover = time.Now()
	entry.UpdatedAt = time.Now()
	idx.Groups[groupKey] = entry

	return m.saveIndexAtomicLocked(idx)
}

// loadIndexLocked reads and parses the index file. Returns an empty index if
// the file does not exist. Caller must hold indexMutex.
func (m *SessionRolloverManager) loadIndexLocked() (*sessionGroupIndex, error) {
	data, err := os.ReadFile(m.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &sessionGroupIndex{Groups: make(map[string]SessionGroupEntry)}, nil
		}
		return nil, fmt.Errorf("read session groups index: %w", err)
	}

	var idx sessionGroupIndex
	if len(data) == 0 {
		return &sessionGroupIndex{Groups: make(map[string]SessionGroupEntry)}, nil
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse session groups index: %w", err)
	}
	if idx.Groups == nil {
		idx.Groups = make(map[string]SessionGroupEntry)
	}
	return &idx, nil
}

// saveIndexAtomicLocked writes the index to a temp file in the same directory
// then renames it over the target. This avoids corrupting the index if two
// agent subprocesses race on the same file. Caller must hold indexMutex.
func (m *SessionRolloverManager) saveIndexAtomicLocked(idx *sessionGroupIndex) error {
	if err := os.MkdirAll(filepath.Dir(m.indexPath), 0o755); err != nil {
		return fmt.Errorf("create session groups dir: %w", err)
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session groups index: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(m.indexPath), sessionGroupsFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp index file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp index file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp index file: %w", err)
	}

	if err := os.Rename(tmpName, m.indexPath); err != nil {
		cleanup()
		return fmt.Errorf("replace session groups index: %w", err)
	}
	return nil
}
