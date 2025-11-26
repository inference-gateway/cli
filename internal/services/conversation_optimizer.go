package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/models"
	"github.com/inference-gateway/sdk"
)

// ConversationOptimizer provides methods to optimize conversation history for token efficiency
type ConversationOptimizer struct {
	enabled    bool
	autoAt     int // percentage of context window (20-100)
	bufferSize int
	client     sdk.Client
	config     *config.Config
	tokenizer  *TokenizerService
}

// OptimizerConfig represents configuration for the conversation optimizer
type OptimizerConfig struct {
	Enabled    bool
	AutoAt     int // percentage of context window (20-100)
	BufferSize int
	Client     sdk.Client
	Config     *config.Config
	Tokenizer  *TokenizerService
}

// NewConversationOptimizer creates a new conversation optimizer with configuration
func NewConversationOptimizer(config OptimizerConfig) *ConversationOptimizer {
	if config.AutoAt < 20 || config.AutoAt > 100 {
		config.AutoAt = 80
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 2
	}

	tokenizer := config.Tokenizer
	if tokenizer == nil {
		tokenizer = NewTokenizerService(DefaultTokenizerConfig())
	}

	return &ConversationOptimizer{
		enabled:    config.Enabled,
		autoAt:     config.AutoAt,
		bufferSize: config.BufferSize,
		client:     config.Client,
		config:     config.Config,
		tokenizer:  tokenizer,
	}
}

// OptimizeMessages reduces token usage by intelligently managing conversation history
func (co *ConversationOptimizer) OptimizeMessages(messages []sdk.Message) []sdk.Message {
	return co.OptimizeMessagesWithModel(messages, "")
}

// OptimizeMessagesWithModel reduces token usage with optional current model for fallback
func (co *ConversationOptimizer) OptimizeMessagesWithModel(messages []sdk.Message, currentModel string) []sdk.Message {
	if !co.enabled || len(messages) == 0 {
		return messages
	}

	// Estimate current token usage
	currentTokens := co.tokenizer.EstimateMessagesTokens(messages)

	// Get context window size for the model
	contextWindow := models.EstimateContextWindow(currentModel)
	if contextWindow == 0 {
		contextWindow = 8192 // fallback
	}

	// Calculate threshold based on percentage
	threshold := (contextWindow * co.autoAt) / 100

	// Only optimize if we've exceeded the threshold
	if currentTokens < threshold {
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

	optimized := co.smartOptimize(conversationMessages, currentModel)
	return append(systemMessages, optimized...)
}

// smartOptimize implements the smart optimization strategy
// It keeps the first user message (root context), summarizes the middle, and preserves recent context
func (co *ConversationOptimizer) smartOptimize(messages []sdk.Message, model string) []sdk.Message {
	if len(messages) < 3 {
		return messages
	}

	var result []sdk.Message
	var firstUserIndex = -1
	for i, msg := range messages {
		if msg.Role == "user" {
			firstUserIndex = i
			result = append(result, msg)
			break
		}
	}

	if firstUserIndex == -1 {
		return messages
	}

	if len(messages) <= co.bufferSize+1 {
		return messages
	}

	lastMessagesStart := len(messages) - co.bufferSize

	for i := lastMessagesStart; i < len(messages); i++ {
		if messages[i].Role == "tool" {
			for j := i - 1; j >= 0; j-- {
				if messages[j].Role == "assistant" && messages[j].ToolCalls != nil && len(*messages[j].ToolCalls) > 0 {
					lastMessagesStart = j
					break
				}
			}
			break
		}
	}

	if lastMessagesStart <= firstUserIndex+1 {
		return messages
	}

	messagesToSummarize := messages[firstUserIndex+1 : lastMessagesStart]

	summary := co.generateSmartSummary(messagesToSummarize, model)
	if summary != "" {
		summaryMsg := sdk.Message{
			Role:    "assistant",
			Content: sdk.NewMessageContent(fmt.Sprintf("[Context Summary: %s]", summary)),
		}
		result = append(result, summaryMsg)
	}

	result = append(result, messages[lastMessagesStart:]...)

	return result
}

// generateSmartSummary creates an intelligent summary using LLM
func (co *ConversationOptimizer) generateSmartSummary(messages []sdk.Message, model string) string {
	if len(messages) == 0 {
		return ""
	}

	if co.client == nil || model == "" {
		return co.createBasicSummary(messages)
	}

	summary, err := co.generateLLMSummary(messages, model)
	if err != nil {
		return co.createBasicSummary(messages)
	}

	return strings.TrimSpace(summary)
}

// createBasicSummary creates a simple summary without LLM
func (co *ConversationOptimizer) createBasicSummary(messages []sdk.Message) string {
	userCount := 0
	assistantCount := 0
	toolCount := 0

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			userCount++
		case "assistant":
			assistantCount++
		case "tool":
			toolCount++
		}
	}

	return fmt.Sprintf("%d user messages, %d assistant responses, %d tool executions were exchanged discussing the task progress.",
		userCount, assistantCount, toolCount)
}

// generateLLMSummary uses the SDK client to generate an intelligent summary
func (co *ConversationOptimizer) generateLLMSummary(messages []sdk.Message, model string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
