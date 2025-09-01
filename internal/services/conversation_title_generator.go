package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/infra/storage"
	"github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// sdkClientWrapper wraps the actual SDK client
type sdkClientWrapper struct {
	client sdk.Client
}

func (w *sdkClientWrapper) WithOptions(opts *sdk.CreateChatCompletionRequest) domain.SDKClient {
	w.client = w.client.WithOptions(opts)
	return w
}

func (w *sdkClientWrapper) WithMiddlewareOptions(opts *sdk.MiddlewareOptions) domain.SDKClient {
	w.client = w.client.WithMiddlewareOptions(opts)
	return w
}

func (w *sdkClientWrapper) WithTools(tools *[]sdk.ChatCompletionTool) domain.SDKClient {
	w.client = w.client.WithTools(tools)
	return w
}

func (w *sdkClientWrapper) GenerateContent(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (*sdk.CreateChatCompletionResponse, error) {
	return w.client.GenerateContent(ctx, provider, model, messages)
}

func (w *sdkClientWrapper) GenerateContentStream(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (<-chan sdk.SSEvent, error) {
	return w.client.GenerateContentStream(ctx, provider, model, messages)
}

// ConversationTitleGenerator generates titles for conversations using AI
type ConversationTitleGenerator struct {
	client  domain.SDKClient
	storage storage.ConversationStorage
	config  *config.Config
}

// NewConversationTitleGenerator creates a new conversation title generator
func NewConversationTitleGenerator(client sdk.Client, storage storage.ConversationStorage, config *config.Config) *ConversationTitleGenerator {
	return &ConversationTitleGenerator{
		client:  &sdkClientWrapper{client: client},
		storage: storage,
		config:  config,
	}
}

// NewConversationTitleGeneratorWithSDKClient creates a new conversation title generator with a custom SDKClient (for testing)
func NewConversationTitleGeneratorWithSDKClient(client domain.SDKClient, storage storage.ConversationStorage, config *config.Config) *ConversationTitleGenerator {
	return &ConversationTitleGenerator{
		client:  client,
		storage: storage,
		config:  config,
	}
}

// GenerateTitleForConversation generates a title for a specific conversation
func (g *ConversationTitleGenerator) GenerateTitleForConversation(ctx context.Context, conversationID string) error {
	if !g.config.Conversation.TitleGeneration.Enabled {
		return nil
	}

	entries, metadata, err := g.storage.LoadConversation(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("failed to load conversation %s: %w", conversationID, err)
	}

	if len(entries) == 0 {
		return nil
	}

	title, err := g.generateTitle(ctx, entries)
	if err != nil {
		return fmt.Errorf("failed to generate title for conversation %s: %w", conversationID, err)
	}

	if strings.TrimSpace(title) == "" {
		title = g.fallbackTitle(entries)
	}

	now := time.Now()
	metadata.Title = title
	metadata.TitleGenerated = true
	metadata.TitleInvalidated = false
	metadata.TitleGenerationTime = &now
	metadata.UpdatedAt = now

	if err := g.storage.UpdateConversationMetadata(ctx, conversationID, metadata); err != nil {
		return fmt.Errorf("failed to update conversation metadata: %w", err)
	}

	logger.Info("Generated title for conversation", "id", conversationID, "title", title)
	return nil
}

// ProcessPendingTitles processes a batch of conversations that need title generation
func (g *ConversationTitleGenerator) ProcessPendingTitles(ctx context.Context) error {
	if !g.config.Conversation.TitleGeneration.Enabled {
		return nil
	}

	batchSize := g.config.Conversation.TitleGeneration.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}

	conversations, err := g.storage.ListConversationsNeedingTitles(ctx, batchSize)
	if err != nil {
		return fmt.Errorf("failed to list conversations needing titles: %w", err)
	}

	if len(conversations) == 0 {
		return nil
	}

	logger.Info("Processing conversations for title generation", "count", len(conversations))

	for _, conv := range conversations {
		if err := g.GenerateTitleForConversation(ctx, conv.ID); err != nil {
			logger.Error("Failed to generate title for conversation", "id", conv.ID, "error", err)
			continue
		}

		select {
		case <-ctx.Done():
			logger.Info("Title generation cancelled", "processed", conv.ID)
			return ctx.Err()
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// InvalidateTitle marks a conversation title as needing regeneration
func (g *ConversationTitleGenerator) InvalidateTitle(ctx context.Context, conversationID string) error {
	_, metadata, err := g.storage.LoadConversation(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("failed to load conversation %s: %w", conversationID, err)
	}

	metadata.TitleInvalidated = true
	metadata.UpdatedAt = time.Now()

	if err := g.storage.UpdateConversationMetadata(ctx, conversationID, metadata); err != nil {
		return fmt.Errorf("failed to invalidate conversation title: %w", err)
	}

	return nil
}

// generateTitle uses AI to generate a conversation title
func (g *ConversationTitleGenerator) generateTitle(ctx context.Context, entries []domain.ConversationEntry) (string, error) {
	if g.client == nil {
		return "", fmt.Errorf("AI client not available")
	}

	model := g.config.Conversation.TitleGeneration.Model
	if model == "" {
		model = g.config.Agent.Model
	}
	if model == "" {
		return "", fmt.Errorf("no model configured for conversation titles")
	}

	systemPrompt := g.config.Conversation.TitleGeneration.SystemPrompt

	conversationText := g.formatConversationForTitleGeneration(entries)
	if conversationText == "" {
		return "", fmt.Errorf("no conversation content available for title generation")
	}

	messages := []sdk.Message{
		{Role: sdk.System, Content: systemPrompt},
		{Role: sdk.User, Content: fmt.Sprintf("Generate a title for this conversation:\n\n%s", conversationText)},
	}

	slashIndex := strings.Index(model, "/")
	if slashIndex == -1 {
		return "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	provider := model[:slashIndex]
	modelName := strings.TrimPrefix(model, provider+"/")
	providerType := sdk.Provider(provider)

	response, err := g.client.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens: &[]int{50}[0],
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
			SkipA2A: true,
		}).
		GenerateContent(ctx, providerType, modelName, messages)
	if err != nil {
		return "", fmt.Errorf("failed to generate conversation title: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no conversation title generated")
	}

	title := strings.TrimSpace(response.Choices[0].Message.Content)
	title = strings.Trim(title, `"'`)

	if len(title) > 50 {
		title = title[:50]
		if lastSpace := strings.LastIndex(title, " "); lastSpace > 30 {
			title = title[:lastSpace]
		}
	}

	return title, nil
}

// formatConversationForTitleGeneration formats conversation entries for AI processing
func (g *ConversationTitleGenerator) formatConversationForTitleGeneration(entries []domain.ConversationEntry) string {
	var content strings.Builder
	maxLength := 2000

	for _, entry := range entries {
		if entry.Hidden {
			continue
		}

		var role string
		switch entry.Message.Role {
		case sdk.User:
			role = "User"
		case sdk.Assistant:
			role = "Assistant"
		default:
			continue
		}

		messageText := strings.TrimSpace(entry.Message.Content)
		if messageText == "" {
			continue
		}

		if len(messageText) > 200 {
			messageText = messageText[:200] + "..."
		}

		line := fmt.Sprintf("%s: %s\n", role, messageText)
		if content.Len()+len(line) > maxLength {
			break
		}
		content.WriteString(line)
	}

	return strings.TrimSpace(content.String())
}

// fallbackTitle creates a fallback title from the first 10 words of the conversation
func (g *ConversationTitleGenerator) fallbackTitle(entries []domain.ConversationEntry) string {
	for _, entry := range entries {
		if entry.Hidden {
			continue
		}

		if entry.Message.Role == sdk.User && strings.TrimSpace(entry.Message.Content) != "" {
			return g.createTitleFromContent(entry.Message.Content)
		}
	}

	return "Conversation"
}

// createTitleFromContent creates a title from message content using first 10 words
func (g *ConversationTitleGenerator) createTitleFromContent(content string) string {
	words := strings.Fields(strings.TrimSpace(content))
	if len(words) == 0 {
		return "Conversation"
	}

	title := ""
	for i, word := range words {
		if i >= 10 {
			break
		}
		if title != "" {
			title += " "
		}
		title += word
	}

	if len(title) > 50 {
		title = title[:50]
		if lastSpace := strings.LastIndex(title, " "); lastSpace > 30 {
			title = title[:lastSpace]
		}
	}

	return title
}
