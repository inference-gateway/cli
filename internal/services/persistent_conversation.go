package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/storage"
)

// PersistentConversationRepository wraps the InMemoryConversationRepository
// and adds persistence capabilities using a storage backend
type PersistentConversationRepository struct {
	*InMemoryConversationRepository
	storage        storage.ConversationStorage
	conversationID string
	metadata       storage.ConversationMetadata
	autoSave       bool
}

// NewPersistentConversationRepository creates a new persistent conversation repository
func NewPersistentConversationRepository(formatterService *ToolFormatterService, storageBackend storage.ConversationStorage) *PersistentConversationRepository {
	inMemory := NewInMemoryConversationRepository(formatterService)

	return &PersistentConversationRepository{
		InMemoryConversationRepository: inMemory,
		storage:                        storageBackend,
		conversationID:                 "", // Will be set when starting a new conversation
		autoSave:                       true,
		metadata: storage.ConversationMetadata{
			Title:        "New Conversation",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			MessageCount: 0,
			TokenStats:   domain.SessionTokenStats{},
			Tags:         []string{},
		},
	}
}

// StartNewConversation begins a new conversation with a unique ID
func (r *PersistentConversationRepository) StartNewConversation(title string) error {
	// Generate unique conversation ID
	r.conversationID = uuid.New().String()

	// Initialize metadata
	now := time.Now()
	r.metadata = storage.ConversationMetadata{
		ID:           r.conversationID,
		Title:        title,
		CreatedAt:    now,
		UpdatedAt:    now,
		MessageCount: 0,
		TokenStats:   domain.SessionTokenStats{},
		Tags:         []string{},
	}

	// Clear in-memory conversation
	return r.InMemoryConversationRepository.Clear()
}

// LoadConversation loads a conversation from persistent storage
func (r *PersistentConversationRepository) LoadConversation(ctx context.Context, conversationID string) error {
	entries, metadata, err := r.storage.LoadConversation(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("failed to load conversation %s: %w", conversationID, err)
	}

	// Clear current conversation
	if err := r.InMemoryConversationRepository.Clear(); err != nil {
		return fmt.Errorf("failed to clear current conversation: %w", err)
	}

	// Load entries into in-memory storage
	for _, entry := range entries {
		if err := r.InMemoryConversationRepository.AddMessage(entry); err != nil {
			return fmt.Errorf("failed to add message to in-memory storage: %w", err)
		}
	}

	// Update current conversation metadata
	r.conversationID = conversationID
	r.metadata = metadata

	// Restore session stats
	r.InMemoryConversationRepository.sessionStats = metadata.TokenStats

	return nil
}

// SaveConversation saves the current conversation to persistent storage
func (r *PersistentConversationRepository) SaveConversation(ctx context.Context) error {
	if r.conversationID == "" {
		return fmt.Errorf("no active conversation to save")
	}

	entries := r.InMemoryConversationRepository.GetMessages()

	// Update metadata
	r.metadata.UpdatedAt = time.Now()
	r.metadata.MessageCount = len(entries)
	r.metadata.TokenStats = r.InMemoryConversationRepository.GetSessionTokens()

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
	// Update real-time stats
	r.metadata.MessageCount = r.InMemoryConversationRepository.GetMessageCount()
	r.metadata.TokenStats = r.InMemoryConversationRepository.GetSessionTokens()
	return r.metadata
}

// SetAutoSave enables or disables automatic saving after each operation
func (r *PersistentConversationRepository) SetAutoSave(enabled bool) {
	r.autoSave = enabled
}

// Override AddMessage to trigger auto-save
func (r *PersistentConversationRepository) AddMessage(msg domain.ConversationEntry) error {
	if err := r.InMemoryConversationRepository.AddMessage(msg); err != nil {
		return err
	}

	if r.autoSave && r.conversationID != "" {
		// Auto-save in background to avoid blocking
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := r.SaveConversation(ctx); err != nil {
				// Log error but don't fail the operation
				// In a real implementation, you might want to use a proper logger
				fmt.Printf("Warning: failed to auto-save conversation: %v\n", err)
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

	// Reset conversation state
	r.conversationID = ""
	now := time.Now()
	r.metadata = storage.ConversationMetadata{
		Title:        "New Conversation",
		CreatedAt:    now,
		UpdatedAt:    now,
		MessageCount: 0,
		TokenStats:   domain.SessionTokenStats{},
		Tags:         []string{},
	}

	return nil
}

// Override AddTokenUsage to trigger auto-save
func (r *PersistentConversationRepository) AddTokenUsage(inputTokens, outputTokens, totalTokens int) error {
	if err := r.InMemoryConversationRepository.AddTokenUsage(inputTokens, outputTokens, totalTokens); err != nil {
		return err
	}

	if r.autoSave && r.conversationID != "" {
		// Auto-save in background
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := r.SaveConversation(ctx); err != nil {
				fmt.Printf("Warning: failed to auto-save conversation after token usage: %v\n", err)
			}
		}()
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
