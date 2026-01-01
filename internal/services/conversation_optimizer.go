package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	logger "github.com/inference-gateway/cli/internal/logger"
	models "github.com/inference-gateway/cli/internal/models"
	sdk "github.com/inference-gateway/sdk"
)

// ConversationOptimizer provides methods to optimize conversation history for token efficiency
type ConversationOptimizer struct {
	enabled           bool
	autoAt            int
	bufferSize        int
	keepFirstMessages int
	client            sdk.Client
	config            *config.Config
	tokenizer         *TokenizerService
}

// OptimizerConfig represents configuration for the conversation optimizer
type OptimizerConfig struct {
	Enabled           bool
	AutoAt            int
	BufferSize        int
	KeepFirstMessages int
	Client            sdk.Client
	Config            *config.Config
	Tokenizer         *TokenizerService
}

// NewConversationOptimizer creates a new conversation optimizer with configuration
func NewConversationOptimizer(config OptimizerConfig) *ConversationOptimizer {
	if config.AutoAt < 20 || config.AutoAt > 100 {
		logger.Warn("AutoAt must be between 20 and 100, defaulting to 80", "provided", config.AutoAt)
		config.AutoAt = 80
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 2
	}
	if config.KeepFirstMessages <= 0 {
		config.KeepFirstMessages = 2
	}

	tokenizer := config.Tokenizer
	if tokenizer == nil {
		tokenizer = NewTokenizerService(DefaultTokenizerConfig())
	}

	return &ConversationOptimizer{
		enabled:           config.Enabled,
		autoAt:            config.AutoAt,
		bufferSize:        config.BufferSize,
		keepFirstMessages: config.KeepFirstMessages,
		client:            config.Client,
		config:            config.Config,
		tokenizer:         tokenizer,
	}
}

// OptimizeMessages reduces token usage by intelligently managing conversation history with LLM summarization
func (co *ConversationOptimizer) OptimizeMessages(messages []sdk.Message, model string, force bool) []sdk.Message {
	if len(messages) == 0 {
		return messages
	}

	if !force && !co.enabled {
		return messages
	}

	currentTokens := co.tokenizer.EstimateMessagesTokens(messages)

	contextWindow := models.EstimateContextWindow(model)
	if contextWindow == 0 {
		contextWindow = 30000
	}

	threshold := (contextWindow * co.autoAt) / 100

	if !force && currentTokens < threshold {
		return messages
	}

	var systemMessages []sdk.Message
	var conversationMessages []sdk.Message

	for _, msg := range messages {
		if msg.Role == "system" {
			systemMessages = append(systemMessages, msg)
		} else {
			conversationMessages = append(conversationMessages, msg)
		}
	}

	optimized, err := co.smartOptimize(conversationMessages, model)
	if err != nil {
		logger.Error("Optimization failed", "error", err)
		// If optimization fails, return original messages
		return messages
	}
	return append(systemMessages, optimized...)
}

// smartOptimize implements the smart optimization strategy
// It keeps the first N messages (default 2) and summarizes the rest
func (co *ConversationOptimizer) smartOptimize(messages []sdk.Message, model string) ([]sdk.Message, error) {
	minMessages := co.keepFirstMessages + 1
	if len(messages) < minMessages {
		return messages, nil
	}

	result := make([]sdk.Message, co.keepFirstMessages)
	copy(result, messages[:co.keepFirstMessages])

	summaryStartIndex := co.keepFirstMessages

	summaryStartIndex = co.adjustBoundaryForToolCallsAtStart(messages, summaryStartIndex)

	if summaryStartIndex >= len(messages) {
		return messages, nil
	}

	messagesToSummarize := messages[summaryStartIndex:]

	if len(messagesToSummarize) == 0 {
		return messages, nil
	}

	if co.client == nil {
		return nil, fmt.Errorf("LLM client is required for conversation compaction")
	}
	if model == "" {
		return nil, fmt.Errorf("model is required for conversation compaction")
	}

	logger.Info("Generating LLM summary for compaction", "model", model, "messages_to_summarize", len(messagesToSummarize))
	summary, err := co.GenerateLLMSummary(messagesToSummarize, model)
	if err != nil {
		logger.Error("Failed to generate LLM summary", "error", err)
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	logger.Info("LLM summary generated successfully", "summary_length", len(summary))

	if summary != "" {
		wrappedSummary := formatting.WrapText(summary, 80)
		formattedSummary := fmt.Sprintf("--- Context Summary ---\n\n%s\n\n--- End Summary ---", wrappedSummary)
		summaryMsg := sdk.Message{
			Role:    "assistant",
			Content: sdk.NewMessageContent(formattedSummary),
		}
		result = append(result, summaryMsg)
	}

	return result, nil
}

// adjustBoundaryForToolCallsAtStart ensures tool call/response pairs aren't split
// at the start boundary. If the last kept message has tool calls with responses
// beyond the boundary, we need to include those responses before summarization.
func (co *ConversationOptimizer) adjustBoundaryForToolCallsAtStart(messages []sdk.Message, boundaryIndex int) int {
	if boundaryIndex <= 0 || boundaryIndex >= len(messages) {
		return boundaryIndex
	}

	lastKeptMsg := messages[boundaryIndex-1]
	if lastKeptMsg.Role != "assistant" || lastKeptMsg.ToolCalls == nil || len(*lastKeptMsg.ToolCalls) == 0 {
		return boundaryIndex
	}

	toolCallIDs := make(map[string]bool)
	for _, tc := range *lastKeptMsg.ToolCalls {
		if tc.Id != "" {
			toolCallIDs[tc.Id] = true
		}
	}

	adjustedBoundary := boundaryIndex
	for i := boundaryIndex; i < len(messages); i++ {
		if messages[i].Role == "tool" && messages[i].ToolCallId != nil {
			if toolCallIDs[*messages[i].ToolCallId] {
				adjustedBoundary = i + 1
				delete(toolCallIDs, *messages[i].ToolCallId)
			}
		} else if messages[i].Role == "assistant" || messages[i].Role == "user" {
			break
		}
	}

	if len(toolCallIDs) > 0 {
		logger.Warn("Found unresolved tool calls at compaction boundary", "count", len(toolCallIDs))
	}

	return adjustedBoundary
}

// GenerateLLMSummary creates a concise summary of conversation messages using an LLM.
// It uses the SDK client to generate an intelligent summary focused on key tasks,
// decisions, critical context, and next steps. The summary is limited to 2-3 sentences.
func (co *ConversationOptimizer) GenerateLLMSummary(messages []sdk.Message, model string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	summaryMessages := make([]sdk.Message, 0, len(messages)+2)

	summaryMessages = append(summaryMessages, sdk.Message{
		Role: sdk.System,
		Content: sdk.NewMessageContent(`You are a conversation summarizer. Create a concise summary that preserves the essential context and progress made in the conversation.

Focus on:
- Key tasks completed or in progress
- Important decisions or findings
- Critical context needed to continue the conversation
- Any unresolved issues or next steps

Keep the summary brief but informative (2-3 sentences max).`),
	})

	for _, msg := range messages {
		switch msg.Role {
		case sdk.User, sdk.Assistant:
			contentStr, err := msg.Content.AsMessageContent0()
			if err != nil {
				contentStr = ""
			}
			if len(contentStr) > 2000 {
				contentStr = contentStr[:2000] + "... [truncated]"
			}

			summaryMessages = append(summaryMessages, sdk.Message{
				Role:    msg.Role,
				Content: sdk.NewMessageContent(contentStr),
			})
		case "tool":
			contentStr, err := msg.Content.AsMessageContent0()
			if err != nil {
				contentStr = ""
			}
			if len(contentStr) > 500 {
				contentStr = contentStr[:500] + "... [tool output truncated]"
			}
			summaryMessages = append(summaryMessages, sdk.Message{
				Role:    "assistant",
				Content: sdk.NewMessageContent(fmt.Sprintf("[Tool result: %s]", contentStr)),
			})
		}
	}

	summaryMessages = append(summaryMessages, sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent("Provide a concise summary of the conversation above, focusing on key progress and context needed to continue."),
	})

	if model == "" {
		return "", fmt.Errorf("no model configured for summarization")
	}

	slashIndex := strings.Index(model, "/")
	if slashIndex == -1 {
		return "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}
	provider := model[:slashIndex]
	modelName := model[slashIndex+1:]

	maxTokens := 200
	options := &sdk.CreateChatCompletionRequest{
		MaxTokens: &maxTokens,
	}

	response, err := co.client.
		WithOptions(options).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
		}).
		GenerateContent(ctx, sdk.Provider(provider), modelName, summaryMessages)

	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no summary generated")
	}

	contentStr, err := response.Choices[0].Message.Content.AsMessageContent0()
	if err != nil {
		return "", fmt.Errorf("failed to extract summary content: %w", err)
	}
	return strings.TrimSpace(contentStr), nil
}
