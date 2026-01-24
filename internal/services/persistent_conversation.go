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
	metadataMutex  sync.RWMutex
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

// StartNewConversation saves the current conversation (if any), then begins a new conversation with a unique ID
func (r *PersistentConversationRepository) StartNewConversation(title string) error {
	r.metadataMutex.RLock()
	hasExistingConversation := r.conversationID != ""
	existingConversationID := r.conversationID
	r.metadataMutex.RUnlock()

	if hasExistingConversation && r.GetMessageCount() > 0 {
		ctx := context.Background()
		if err := r.SaveConversation(ctx); err != nil {
			logger.Warn("Failed to save current conversation before starting new one", "error", err, "conversation_id", existingConversationID)
		}
	}

	if err := r.InMemoryConversationRepository.Clear(); err != nil {
		return fmt.Errorf("failed to clear in-memory conversation: %w", err)
	}

	conversationID := uuid.New().String()
	now := time.Now()

	r.metadataMutex.Lock()
	r.conversationID = conversationID
	r.metadata = storage.ConversationMetadata{
		ID:               conversationID,
		Title:            title,
		CreatedAt:        now,
		UpdatedAt:        now,
		MessageCount:     0,
		TokenStats:       domain.SessionTokenStats{},
		Tags:             []string{},
		TitleGenerated:   false,
		TitleInvalidated: false,
	}
	r.metadataMutex.Unlock()

	if r.taskTracker != nil {
		r.taskTracker.ClearAllAgents()
	}

	return nil
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

	r.metadataMutex.Lock()
	r.conversationID = conversationID
	r.metadata = metadata
	r.metadataMutex.Unlock()

	r.SetSessionStats(metadata.TokenStats, metadata.CostStats)

	if r.taskTracker != nil {
		r.taskTracker.ClearAllAgents()
	}

	return nil
}

// SaveConversation saves the current conversation to persistent storage
func (r *PersistentConversationRepository) SaveConversation(ctx context.Context) error {
	r.metadataMutex.RLock()
	conversationID := r.conversationID
	r.metadataMutex.RUnlock()

	if conversationID == "" {
		return fmt.Errorf("no active conversation to save")
	}

	allEntries := r.GetMessages()
	tokenStats := r.GetSessionTokens()
	costStats := r.GetSessionCostStats()

	entries := make([]domain.ConversationEntry, 0, len(allEntries))
	for _, entry := range allEntries {
		if entry.PendingToolCall == nil {
			entries = append(entries, entry)
		}
	}

	r.metadataMutex.Lock()
	r.metadata.UpdatedAt = time.Now()
	r.metadata.MessageCount = len(entries)
	r.metadata.TokenStats = tokenStats
	r.metadata.CostStats = costStats
	metadata := r.metadata
	r.metadataMutex.Unlock()

	return r.storage.SaveConversation(ctx, conversationID, entries, metadata)
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
	r.metadataMutex.Lock()
	defer r.metadataMutex.Unlock()
	r.metadata.Title = title
	r.metadata.UpdatedAt = time.Now()
}

// SetConversationTags sets tags for the current conversation
func (r *PersistentConversationRepository) SetConversationTags(tags []string) {
	r.metadataMutex.Lock()
	defer r.metadataMutex.Unlock()
	r.metadata.Tags = tags
	r.metadata.UpdatedAt = time.Now()
}

// GetCurrentConversationID returns the current conversation ID
func (r *PersistentConversationRepository) GetCurrentConversationID() string {
	r.metadataMutex.RLock()
	defer r.metadataMutex.RUnlock()
	return r.conversationID
}

// GetCurrentConversationMetadata returns the current conversation metadata
func (r *PersistentConversationRepository) GetCurrentConversationMetadata() storage.ConversationMetadata {
	messageCount := r.GetMessageCount()
	tokenStats := r.GetSessionTokens()

	r.metadataMutex.Lock()
	defer r.metadataMutex.Unlock()
	r.metadata.MessageCount = messageCount
	r.metadata.TokenStats = tokenStats
	return r.metadata
}

// SetAutoSave enables or disables automatic saving after each operation
func (r *PersistentConversationRepository) SetAutoSave(enabled bool) {
	r.autoSave = enabled
}

// Override AddMessage to trigger auto-save
func (r *PersistentConversationRepository) AddMessage(msg domain.ConversationEntry) error {
	r.metadataMutex.RLock()
	wasExistingConversation := r.conversationID != ""
	needsInit := r.autoSave && r.conversationID == ""
	r.metadataMutex.RUnlock()

	if needsInit {
		conversationID := uuid.New().String()
		now := time.Now()

		title := "New Conversation"
		if msg.Message.Role == sdk.User {
			contentStr, _ := msg.Message.Content.AsMessageContent0()
			title = domain.CreateTitleFromMessage(contentStr)
		}

		r.metadataMutex.Lock()
		r.conversationID = conversationID
		r.metadata = storage.ConversationMetadata{
			ID:           conversationID,
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
		r.metadataMutex.Unlock()
	}

	if err := r.InMemoryConversationRepository.AddMessage(msg); err != nil {
		return err
	}

	r.metadataMutex.RLock()
	titleGenerated := r.metadata.TitleGenerated
	conversationIDForInvalidation := r.conversationID
	r.metadataMutex.RUnlock()

	if wasExistingConversation && titleGenerated && r.titleGenerator != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := r.titleGenerator.InvalidateTitle(ctx, conversationIDForInvalidation); err != nil {
				logger.Warn("Failed to invalidate conversation title", "error", err, "conversationID", conversationIDForInvalidation)
			}
		}()
	}

	r.metadataMutex.RLock()
	shouldAutoSave := r.autoSave && r.conversationID != ""
	r.metadataMutex.RUnlock()

	if shouldAutoSave {
		r.autoSaveMutex.Lock()
		defer r.autoSaveMutex.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := r.SaveConversation(ctx); err != nil {
			logger.Warn("Failed to auto-save conversation", "error", err)
			return err
		}
	}

	return nil
}

// Override Clear to handle conversation state
func (r *PersistentConversationRepository) Clear() error {
	if err := r.InMemoryConversationRepository.Clear(); err != nil {
		return err
	}

	now := time.Now()

	r.metadataMutex.Lock()
	r.conversationID = ""
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
	r.metadataMutex.Unlock()

	return nil
}

// DeleteMessagesAfterIndex wraps the in-memory implementation with auto-save
func (r *PersistentConversationRepository) DeleteMessagesAfterIndex(index int) error {
	if err := r.InMemoryConversationRepository.DeleteMessagesAfterIndex(index); err != nil {
		return err
	}

	r.metadataMutex.RLock()
	shouldAutoSave := r.autoSave && r.conversationID != ""
	r.metadataMutex.RUnlock()

	if shouldAutoSave {
		r.autoSaveMutex.Lock()
		defer r.autoSaveMutex.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := r.SaveConversation(ctx); err != nil {
			logger.Warn("Failed to auto-save conversation after deleting messages", "error", err)
			return err
		}
	}

	return nil
}

// AddTokenUsage wraps the in-memory implementation with persistence and auto-save
func (r *PersistentConversationRepository) AddTokenUsage(model string, inputTokens, outputTokens, totalTokens int) error {
	r.metadataMutex.RLock()
	needsInit := r.autoSave && r.conversationID == ""
	r.metadataMutex.RUnlock()

	if needsInit {
		conversationID := uuid.New().String()
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

		r.metadataMutex.Lock()
		r.conversationID = conversationID
		r.metadata = storage.ConversationMetadata{
			ID:           conversationID,
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
		r.metadataMutex.Unlock()
	}

	if err := r.InMemoryConversationRepository.AddTokenUsage(model, inputTokens, outputTokens, totalTokens); err != nil {
		return err
	}

	r.metadataMutex.RLock()
	shouldAutoSave := r.autoSave && r.conversationID != ""
	r.metadataMutex.RUnlock()

	if shouldAutoSave {
		r.autoSaveMutex.Lock()
		defer r.autoSaveMutex.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := r.SaveConversation(ctx); err != nil {
			logger.Warn("Failed to auto-save conversation after token usage", "error", err)
			return err
		}
	}

	return nil
}

// Close closes the storage connection
func (r *PersistentConversationRepository) Close() error {
	if r.storage != nil {
		return r.storage.Close()
	}
	return nil
}

// GetCurrentConversationTitle returns the current conversation title
func (r *PersistentConversationRepository) GetCurrentConversationTitle() string {
	r.metadataMutex.RLock()
	defer r.metadataMutex.RUnlock()
	return r.metadata.Title
}
