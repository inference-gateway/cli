package storage

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
)

// MemoryStorage implements ConversationStorage using in-memory storage
// This allows conversation history features to work without persistent storage
type MemoryStorage struct {
	conversations map[string]conversationData
	mutex         sync.RWMutex
}

type conversationData struct {
	entries  []domain.ConversationEntry
	metadata ConversationMetadata
}

// NewMemoryStorage creates a new in-memory storage instance
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		conversations: make(map[string]conversationData),
	}
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
			Summary:             data.metadata.Summary,
			TitleGenerated:      data.metadata.TitleGenerated,
			TitleInvalidated:    data.metadata.TitleInvalidated,
			TitleGenerationTime: data.metadata.TitleGenerationTime,
		}
		summaries = append(summaries, summary)
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
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
	return nil
}

// Health checks if the storage is healthy and reachable
func (m *MemoryStorage) Health(ctx context.Context) error {
	return nil
}
