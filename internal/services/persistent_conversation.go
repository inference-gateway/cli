package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	uuid "github.com/google/uuid"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// PersistentConversationRepository wraps the InMemoryConversationRepository
// and adds persistence capabilities using a storage backend
type PersistentConversationRepository struct {
	*InMemoryConversationRepository
	storage        storage.ConversationStorage
	conversationID string
	metadata       storage.ConversationMetadata
	autoSave       bool
	titleGenerator *ConversationTitleGenerator
	autoSaveMutex  sync.Mutex
	taskTracker    domain.TaskTracker
}

// NewPersistentConversationRepository creates a new persistent conversation repository
func NewPersistentConversationRepository(formatterService *ToolFormatterService, pricingService domain.PricingService, storageBackend storage.ConversationStorage) *PersistentConversationRepository {
	inMemory := NewInMemoryConversationRepository(formatterService, pricingService)

	return &PersistentConversationRepository{
		InMemoryConversationRepository: inMemory,
		storage:                        storageBackend,
		conversationID:                 "",
		autoSave:                       true,
		metadata: storage.ConversationMetadata{
			Title:            "New Conversation",
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
			MessageCount:     0,
			TokenStats:       domain.SessionTokenStats{},
			Tags:             []string{},
			TitleGenerated:   false,
			TitleInvalidated: false,
		},
	}
}

// SetTitleGenerator sets the title generator for automatic title invalidation
func (r *PersistentConversationRepository) SetTitleGenerator(titleGenerator *ConversationTitleGenerator) {
	r.titleGenerator = titleGenerator
}

// SetTaskTracker sets the task tracker for context ID persistence
func (r *PersistentConversationRepository) SetTaskTracker(taskTracker domain.TaskTracker) {
	r.taskTracker = taskTracker
}

// StartNewConversation begins a new conversation with a unique ID
func (r *PersistentConversationRepository) StartNewConversation(title string) error {
	r.conversationID = uuid.New().String()

	now := time.Now()
	r.metadata = storage.ConversationMetadata{
		ID:               r.conversationID,
		Title:            title,
		CreatedAt:        now,
		UpdatedAt:        now,
		MessageCount:     0,
		TokenStats:       domain.SessionTokenStats{},
		Tags:             []string{},
		TitleGenerated:   false,
		TitleInvalidated: false,
	}

	if r.taskTracker != nil {
		r.taskTracker.ClearAllAgents()
	}

	return r.InMemoryConversationRepository.Clear()
}

// LoadConversation loads a conversation from persistent storage
func (r *PersistentConversationRepository) LoadConversation(ctx context.Context, conversationID string) error {
	entries, metadata, err := r.storage.LoadConversation(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("failed to load conversation %s: %w", conversationID, err)
	}

	if err := r.InMemoryConversationRepository.Clear(); err != nil {
		return fmt.Errorf("failed to clear current conversation: %w", err)
	}

	for _, entry := range entries {
		if err := r.InMemoryConversationRepository.AddMessage(entry); err != nil {
			return fmt.Errorf("failed to add message to in-memory storage: %w", err)
		}
	}

	r.conversationID = conversationID
	r.metadata = metadata

	r.sessionStats = metadata.TokenStats
	r.costStats = metadata.CostStats

	if r.taskTracker != nil {
		r.taskTracker.ClearAllAgents()
	}

	return nil
}

// SaveConversation saves the current conversation to persistent storage
func (r *PersistentConversationRepository) SaveConversation(ctx context.Context) error {
	if r.conversationID == "" {
		return fmt.Errorf("no active conversation to save")
	}

	entries := r.GetMessages()

	r.metadata.UpdatedAt = time.Now()
	r.metadata.MessageCount = len(entries)
	r.metadata.TokenStats = r.GetSessionTokens()
	r.metadata.CostStats = r.GetSessionCostStats()

	return r.storage.SaveConversation(ctx, r.conversationID, entries, r.metadata)
}

// ListSavedConversations returns a list of saved conversations
func (r *PersistentConversationRepository) ListSavedConversations(ctx context.Context, limit, offset int) ([]storage.ConversationSummary, error) {
	return r.storage.ListConversations(ctx, limit, offset)
}

// DeleteSavedConversation deletes a saved conversation
func (r *PersistentConversationRepository) DeleteSavedConversation(ctx context.Context, conversationID string) error {
	return r.storage.DeleteConversation(ctx, conversationID)
}

// SetConversationTitle sets the title for the current conversation
func (r *PersistentConversationRepository) SetConversationTitle(title string) {
	r.metadata.Title = title
	r.metadata.UpdatedAt = time.Now()
}

// SetConversationTags sets tags for the current conversation
func (r *PersistentConversationRepository) SetConversationTags(tags []string) {
	r.metadata.Tags = tags
	r.metadata.UpdatedAt = time.Now()
}

// GetCurrentConversationID returns the current conversation ID
func (r *PersistentConversationRepository) GetCurrentConversationID() string {
	return r.conversationID
}

// GetCurrentConversationMetadata returns the current conversation metadata
func (r *PersistentConversationRepository) GetCurrentConversationMetadata() storage.ConversationMetadata {
	r.metadata.MessageCount = r.GetMessageCount()
	r.metadata.TokenStats = r.GetSessionTokens()
	return r.metadata
}

// SetAutoSave enables or disables automatic saving after each operation
func (r *PersistentConversationRepository) SetAutoSave(enabled bool) {
	r.autoSave = enabled
}

// Override AddMessage to trigger auto-save
func (r *PersistentConversationRepository) AddMessage(msg domain.ConversationEntry) error {
	wasExistingConversation := r.conversationID != ""

	if r.autoSave && r.conversationID == "" {
		r.conversationID = uuid.New().String()
		now := time.Now()

		title := "New Conversation"
		if msg.Message.Role == sdk.User {
			contentStr, _ := msg.Message.Content.AsMessageContent0()
			title = domain.CreateTitleFromMessage(contentStr)
		}

		r.metadata = storage.ConversationMetadata{
			ID:           r.conversationID,
			Title:        title,
			CreatedAt:    now,
			UpdatedAt:    now,
			MessageCount: 0,
			TokenStats:   domain.SessionTokenStats{},
			CostStats: domain.SessionCostStats{
				PerModelStats: make(map[string]*domain.ModelCostStats),
				Currency:      "USD",
			},
			Tags:             []string{},
			TitleGenerated:   false,
			TitleInvalidated: false,
		}
	}

	if err := r.InMemoryConversationRepository.AddMessage(msg); err != nil {
		return err
	}

	if wasExistingConversation && r.metadata.TitleGenerated && r.titleGenerator != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := r.titleGenerator.InvalidateTitle(ctx, r.conversationID); err != nil {
				logger.Warn("Failed to invalidate conversation title", "error", err, "conversationID", r.conversationID)
			}
		}()
	}

	if r.autoSave && r.conversationID != "" {
		go func() {
			r.autoSaveMutex.Lock()
			defer r.autoSaveMutex.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := r.SaveConversation(ctx); err != nil {
				logger.Warn("Failed to auto-save conversation", "error", err)
			}
		}()
	}

	return nil
}

// Override Clear to handle conversation state
func (r *PersistentConversationRepository) Clear() error {
	if err := r.InMemoryConversationRepository.Clear(); err != nil {
		return err
	}

	r.conversationID = ""
	now := time.Now()
	r.metadata = storage.ConversationMetadata{
		Title:            "New Conversation",
		CreatedAt:        now,
		UpdatedAt:        now,
		MessageCount:     0,
		TokenStats:       domain.SessionTokenStats{},
		Tags:             []string{},
		TitleGenerated:   false,
		TitleInvalidated: false,
	}

	return nil
}

// AddTokenUsage wraps the in-memory implementation with persistence and auto-save
func (r *PersistentConversationRepository) AddTokenUsage(model string, inputTokens, outputTokens, totalTokens int) error {
	if r.autoSave && r.conversationID == "" {
		r.conversationID = uuid.New().String()
		now := time.Now()

		title := "New Conversation"
		messages := r.GetMessages()
		for _, entry := range messages {
			if entry.Message.Role == sdk.User {
				contentStr, _ := entry.Message.Content.AsMessageContent0()
				title = domain.CreateTitleFromMessage(contentStr)
				break
			}
		}

		r.metadata = storage.ConversationMetadata{
			ID:           r.conversationID,
			Title:        title,
			CreatedAt:    now,
			UpdatedAt:    now,
			MessageCount: 0,
			TokenStats:   domain.SessionTokenStats{},
			CostStats: domain.SessionCostStats{
				PerModelStats: make(map[string]*domain.ModelCostStats),
				Currency:      "USD",
			},
			Tags:             []string{},
			TitleGenerated:   false,
			TitleInvalidated: false,
		}
	}

	if err := r.InMemoryConversationRepository.AddTokenUsage(model, inputTokens, outputTokens, totalTokens); err != nil {
		return err
	}

	if r.autoSave && r.conversationID != "" {
		go func() {
			r.autoSaveMutex.Lock()
			defer r.autoSaveMutex.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := r.SaveConversation(ctx); err != nil {
				logger.Warn("Failed to auto-save conversation after token usage", "error", err)
			}
		}()
	}

	return nil
}

// GetOptimizedMessages retrieves the stored optimized conversation messages
func (r *PersistentConversationRepository) GetOptimizedMessages() []sdk.Message {
	if len(r.metadata.OptimizedMessages) == 0 {
		return nil
	}

	optimizedMessages := make([]sdk.Message, 0, len(r.metadata.OptimizedMessages))
	for _, entry := range r.metadata.OptimizedMessages {
		optimizedMessages = append(optimizedMessages, sdk.Message{
			Role:       entry.Message.Role,
			Content:    entry.Message.Content,
			ToolCalls:  entry.Message.ToolCalls,
			ToolCallId: entry.Message.ToolCallId,
		})
	}
	return optimizedMessages
}

// SetOptimizedMessages stores the optimized conversation messages
func (r *PersistentConversationRepository) SetOptimizedMessages(ctx context.Context, optimizedMessages []sdk.Message) error {
	if r.conversationID == "" {
		return fmt.Errorf("no active conversation to store optimized messages")
	}

	r.autoSaveMutex.Lock()
	defer r.autoSaveMutex.Unlock()

	conversationEntries := make([]domain.ConversationEntry, 0, len(optimizedMessages))
	now := time.Now()

	for _, msg := range optimizedMessages {
		entry := domain.ConversationEntry{
			Message: domain.Message{
				Role:       msg.Role,
				Content:    msg.Content,
				ToolCalls:  msg.ToolCalls,
				ToolCallId: msg.ToolCallId,
			},
			Time: now,
		}
		conversationEntries = append(conversationEntries, entry)
	}

	r.metadata.OptimizedMessages = conversationEntries
	r.metadata.UpdatedAt = now

	return r.storage.UpdateConversationMetadata(ctx, r.conversationID, r.metadata)
}

// Close closes the storage connection
func (r *PersistentConversationRepository) Close() error {
	if r.storage != nil {
		return r.storage.Close()
	}
	return nil
}
