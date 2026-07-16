package storage

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// MemoryStorage implements ConversationStorage using in-memory storage
// This allows conversation history features to work without persistent storage
type MemoryStorage struct {
	conversations map[string]conversationData
	sessionGroups map[string]SessionGroupEntry
	scheduledJobs map[string]*domain.ScheduledJob
	plans         map[string]*PlanRecord
	shellHistory  []string
	mutex         sync.RWMutex

	// jobWatchers is a set of channels that receive scheduled-job change events.
	// Each channel is registered by Watch() and removed when the caller's ctx
	// is cancelled. Must only be accessed under mutex.
	jobWatchers map[chan<- ScheduledJobChangeEvent]struct{}
}

type conversationData struct {
	entries  []domain.ConversationEntry
	metadata ConversationMetadata
}

// NewMemoryStorage creates a new in-memory storage instance
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		conversations: make(map[string]conversationData),
		sessionGroups: make(map[string]SessionGroupEntry),
	}
}

// NewMemorySessionGroupStorage returns an in-memory SessionGroupStorage. Used
// as a fallback when conversation storage is disabled but the rollover manager
// still needs somewhere to keep group state for the lifetime of the process.
func NewMemorySessionGroupStorage() SessionGroupStorage {
	return NewMemoryStorage()
}

// SaveConversation saves a conversation with a unique ID
func (m *MemoryStorage) SaveConversation(ctx context.Context, conversationID string, entries []domain.ConversationEntry, metadata ConversationMetadata) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	metadata.UpdatedAt = time.Now()
	metadata.MessageCount = len(entries)

	entriesCopy := make([]domain.ConversationEntry, len(entries))
	copy(entriesCopy, entries)

	m.conversations[conversationID] = conversationData{
		entries:  entriesCopy,
		metadata: metadata,
	}

	return nil
}

// LoadConversation loads a conversation by its ID
func (m *MemoryStorage) LoadConversation(ctx context.Context, conversationID string) ([]domain.ConversationEntry, ConversationMetadata, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	data, exists := m.conversations[conversationID]
	if !exists {
		return nil, ConversationMetadata{}, fmt.Errorf("conversation not found: %s", conversationID)
	}

	entriesCopy := make([]domain.ConversationEntry, len(data.entries))
	copy(entriesCopy, data.entries)

	return entriesCopy, data.metadata, nil
}

// ListConversations returns a list of conversation summaries
func (m *MemoryStorage) ListConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	summaries := make([]ConversationSummary, 0, len(m.conversations))

	for _, data := range m.conversations {
		summary := ConversationSummary{
			ID:                  data.metadata.ID,
			Title:               data.metadata.Title,
			CreatedAt:           data.metadata.CreatedAt,
			UpdatedAt:           data.metadata.UpdatedAt,
			MessageCount:        data.metadata.MessageCount,
			TokenStats:          data.metadata.TokenStats,
			Model:               data.metadata.Model,
			Tags:                data.metadata.Tags,
			TitleGenerated:      data.metadata.TitleGenerated,
			TitleInvalidated:    data.metadata.TitleInvalidated,
			TitleGenerationTime: data.metadata.TitleGenerationTime,
		}
		summaries = append(summaries, summary)
	}

	slices.SortFunc(summaries, func(a, b ConversationSummary) int {
		return b.UpdatedAt.Compare(a.UpdatedAt)
	})

	if offset >= len(summaries) {
		return []ConversationSummary{}, nil
	}

	end := min(offset+limit, len(summaries))

	if limit <= 0 {
		return summaries[offset:], nil
	}

	return summaries[offset:end], nil
}

// DeleteConversation removes a conversation by its ID
func (m *MemoryStorage) DeleteConversation(ctx context.Context, conversationID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.conversations[conversationID]; !exists {
		return fmt.Errorf("conversation not found: %s", conversationID)
	}

	delete(m.conversations, conversationID)
	return nil
}

// UpdateConversationMetadata updates metadata for a conversation
func (m *MemoryStorage) UpdateConversationMetadata(ctx context.Context, conversationID string, metadata ConversationMetadata) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	data, exists := m.conversations[conversationID]
	if !exists {
		return fmt.Errorf("conversation not found: %s", conversationID)
	}

	metadata.UpdatedAt = time.Now()
	data.metadata = metadata
	m.conversations[conversationID] = data

	return nil
}

// ListConversationsNeedingTitles returns conversations that need title generation
func (m *MemoryStorage) ListConversationsNeedingTitles(ctx context.Context, limit int) ([]ConversationSummary, error) {
	return []ConversationSummary{}, nil
}

// Close closes the storage connection (no-op for memory storage)
func (m *MemoryStorage) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.conversations = make(map[string]conversationData)
	m.sessionGroups = make(map[string]SessionGroupEntry)
	return nil
}

// GetSessionGroup returns the entry for groupKey or (_, false, nil) if missing.
func (m *MemoryStorage) GetSessionGroup(_ context.Context, groupKey string) (SessionGroupEntry, bool, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	entry, ok := m.sessionGroups[groupKey]
	if !ok {
		return SessionGroupEntry{}, false, nil
	}
	return cloneSessionGroupEntry(entry), true, nil
}

// PutSessionGroup creates or replaces the entry for groupKey.
func (m *MemoryStorage) PutSessionGroup(_ context.Context, groupKey string, entry SessionGroupEntry) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.sessionGroups[groupKey] = cloneSessionGroupEntry(entry)
	return nil
}

// ListSessionGroups returns a copy of all session-group entries.
func (m *MemoryStorage) ListSessionGroups(_ context.Context) (map[string]SessionGroupEntry, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	out := make(map[string]SessionGroupEntry, len(m.sessionGroups))
	for k, v := range m.sessionGroups {
		out[k] = cloneSessionGroupEntry(v)
	}
	return out, nil
}

func cloneSessionGroupEntry(entry SessionGroupEntry) SessionGroupEntry {
	if len(entry.History) == 0 {
		return entry
	}
	historyCopy := make([]string, len(entry.History))
	copy(historyCopy, entry.History)
	entry.History = historyCopy
	return entry
}

// Health checks if the storage is healthy and reachable
func (m *MemoryStorage) Health(ctx context.Context) error {
	return nil
}

// ---------------------------------------------------------------------------
// ScheduledJobStorage (MemoryStorage)
// ---------------------------------------------------------------------------

// SaveJob creates or updates a scheduled job.
func (m *MemoryStorage) SaveJob(ctx context.Context, job *domain.ScheduledJob) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.scheduledJobs == nil {
		m.scheduledJobs = make(map[string]*domain.ScheduledJob)
	}
	m.scheduledJobs[job.ID] = job
	m.emitJobEventLocked(ScheduledJobChangeEvent{ID: job.ID, Type: "save"})
	return nil
}

// LoadJob returns a job by ID.
func (m *MemoryStorage) LoadJob(ctx context.Context, id string) (*domain.ScheduledJob, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	job, ok := m.scheduledJobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	return job, nil
}

// ListJobs returns all jobs sorted by CreatedAt ascending.
func (m *MemoryStorage) ListJobs(ctx context.Context) ([]*domain.ScheduledJob, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	var jobs []*domain.ScheduledJob
	for _, job := range m.scheduledJobs {
		jobs = append(jobs, job)
	}
	slices.SortFunc(jobs, func(a, b *domain.ScheduledJob) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return jobs, nil
}

// DeleteJob removes a job by ID.
func (m *MemoryStorage) DeleteJob(ctx context.Context, id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.scheduledJobs[id]; !ok {
		return ErrJobNotFound
	}
	delete(m.scheduledJobs, id)
	m.emitJobEventLocked(ScheduledJobChangeEvent{ID: id, Type: "delete"})
	return nil
}

// Watch returns a channel that emits change events via in-process broadcast.
func (m *MemoryStorage) Watch(ctx context.Context) <-chan ScheduledJobChangeEvent {
	ch := make(chan ScheduledJobChangeEvent, 64)
	m.mutex.Lock()
	if m.jobWatchers == nil {
		m.jobWatchers = make(map[chan<- ScheduledJobChangeEvent]struct{})
	}
	m.jobWatchers[ch] = struct{}{}
	m.mutex.Unlock()

	go func() {
		<-ctx.Done()
		m.mutex.Lock()
		delete(m.jobWatchers, ch)
		m.mutex.Unlock()
		close(ch)
	}()
	return ch
}

// emitJobEventLocked broadcasts a change event to all active watchers.
// Must be called with m.mutex held.
func (m *MemoryStorage) emitJobEventLocked(ev ScheduledJobChangeEvent) {
	for ch := range m.jobWatchers {
		select {
		case ch <- ev:
		default:
			// Drop if watcher is not reading fast enough.
		}
	}
}

// ---------------------------------------------------------------------------
// PlanStorage (MemoryStorage)
// ---------------------------------------------------------------------------

// SavePlan creates a plan record.
func (m *MemoryStorage) SavePlan(ctx context.Context, plan *PlanRecord) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.plans == nil {
		m.plans = make(map[string]*PlanRecord)
	}
	m.plans[plan.ID] = plan
	return nil
}

// LoadPlan returns a plan by ID.
func (m *MemoryStorage) LoadPlan(ctx context.Context, id string) (*PlanRecord, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	plan, ok := m.plans[id]
	if !ok {
		return nil, fmt.Errorf("plan not found: %s", id)
	}
	return plan, nil
}

// ListPlans returns all plans sorted by CreatedAt descending.
func (m *MemoryStorage) ListPlans(ctx context.Context) ([]*PlanRecord, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	var plans []*PlanRecord
	for _, plan := range m.plans {
		plans = append(plans, plan)
	}
	slices.SortFunc(plans, func(a, b *PlanRecord) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return plans, nil
}

// DeletePlan removes a plan by ID.
func (m *MemoryStorage) DeletePlan(ctx context.Context, id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if _, ok := m.plans[id]; !ok {
		return fmt.Errorf("plan not found: %s", id)
	}
	delete(m.plans, id)
	return nil
}

// ---------------------------------------------------------------------------
// ShellHistoryStorage (MemoryStorage)
// ---------------------------------------------------------------------------

// AppendHistory appends a command to the shell history.
func (m *MemoryStorage) AppendHistory(ctx context.Context, command string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.shellHistory = append(m.shellHistory, command)
	return nil
}

// LoadHistory returns the most recent commands up to limit.
func (m *MemoryStorage) LoadHistory(ctx context.Context, limit int) ([]string, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if len(m.shellHistory) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit >= len(m.shellHistory) {
		result := make([]string, len(m.shellHistory))
		copy(result, m.shellHistory)
		return result, nil
	}
	result := make([]string, limit)
	copy(result, m.shellHistory[len(m.shellHistory)-limit:])
	return result, nil
}
