package container

import (
	"context"

	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/shortcuts"
)

// PersistentConversationAdapter adapts services.PersistentConversationRepository to shortcuts.PersistentConversationRepository
type PersistentConversationAdapter struct {
	repo *services.PersistentConversationRepository
}

// NewPersistentConversationAdapter creates a new adapter
func NewPersistentConversationAdapter(repo *services.PersistentConversationRepository) *PersistentConversationAdapter {
	return &PersistentConversationAdapter{repo: repo}
}

// ListSavedConversations adapts the method to return shortcuts.ConversationSummary
func (a *PersistentConversationAdapter) ListSavedConversations(ctx context.Context, limit, offset int) ([]shortcuts.ConversationSummary, error) {
	storageConversations, err := a.repo.ListSavedConversations(ctx, limit, offset)
	if err != nil {
		return nil, err
	}

	result := make([]shortcuts.ConversationSummary, len(storageConversations))
	for i, conv := range storageConversations {
		result[i] = shortcuts.ConversationSummary{
			ID:           conv.ID,
			Title:        conv.Title,
			CreatedAt:    conv.CreatedAt.Format("2006-01-02 15:04:05"),
			UpdatedAt:    conv.UpdatedAt.Format("2006-01-02 15:04:05"),
			MessageCount: conv.MessageCount,
			TokenStats: shortcuts.TokenStats{
				TotalInputTokens:  conv.TokenStats.TotalInputTokens,
				TotalOutputTokens: conv.TokenStats.TotalOutputTokens,
				TotalTokens:       conv.TokenStats.TotalTokens,
				RequestCount:      conv.TokenStats.RequestCount,
			},
			Model:   conv.Model,
			Tags:    conv.Tags,
			Summary: conv.Summary,
		}
	}

	return result, nil
}

// LoadConversation delegates to the underlying repository
func (a *PersistentConversationAdapter) LoadConversation(ctx context.Context, conversationID string) error {
	return a.repo.LoadConversation(ctx, conversationID)
}

// GetCurrentConversationMetadata adapts the method to return shortcuts.ConversationMetadata
func (a *PersistentConversationAdapter) GetCurrentConversationMetadata() shortcuts.ConversationMetadata {
	metadata := a.repo.GetCurrentConversationMetadata()
	return shortcuts.ConversationMetadata{
		ID:           metadata.ID,
		Title:        metadata.Title,
		CreatedAt:    metadata.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt:    metadata.UpdatedAt.Format("2006-01-02 15:04:05"),
		MessageCount: metadata.MessageCount,
		TokenStats: shortcuts.TokenStats{
			TotalInputTokens:  metadata.TokenStats.TotalInputTokens,
			TotalOutputTokens: metadata.TokenStats.TotalOutputTokens,
			TotalTokens:       metadata.TokenStats.TotalTokens,
			RequestCount:      metadata.TokenStats.RequestCount,
		},
		Model:   metadata.Model,
		Tags:    metadata.Tags,
		Summary: metadata.Summary,
	}
}

// SaveConversation delegates to the underlying repository
func (a *PersistentConversationAdapter) SaveConversation(ctx context.Context) error {
	return a.repo.SaveConversation(ctx)
}

// StartNewConversation delegates to the underlying repository
func (a *PersistentConversationAdapter) StartNewConversation(title string) error {
	return a.repo.StartNewConversation(title)
}

// GetCurrentConversationID delegates to the underlying repository
func (a *PersistentConversationAdapter) GetCurrentConversationID() string {
	return a.repo.GetCurrentConversationID()
}

// SetConversationTitle delegates to the underlying repository
func (a *PersistentConversationAdapter) SetConversationTitle(title string) {
	a.repo.SetConversationTitle(title)
}
