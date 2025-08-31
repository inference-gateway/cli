package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/inference-gateway/sdk"
)

// ConversationOptimizer provides methods to optimize conversation history for token efficiency
type ConversationOptimizer struct {
	enabled     bool
	model       string
	minMessages int
	bufferSize  int
	client      sdk.Client
	config      *config.Config
}

// OptimizerConfig represents configuration for the conversation optimizer
type OptimizerConfig struct {
	Enabled     bool
	Model       string
	MinMessages int
	BufferSize  int
	Client      sdk.Client
	Config      *config.Config
}

// NewConversationOptimizer creates a new conversation optimizer with configuration
func NewConversationOptimizer(config OptimizerConfig) *ConversationOptimizer {
	if config.MinMessages <= 0 {
		config.MinMessages = 10
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 2
	}

	return &ConversationOptimizer{
		enabled:     config.Enabled,
		model:       config.Model,
		minMessages: config.MinMessages,
		bufferSize:  config.BufferSize,
		client:      config.Client,
		config:      config.Config,
	}
}

// OptimizeMessages reduces token usage by intelligently managing conversation history
func (co *ConversationOptimizer) OptimizeMessages(messages []sdk.Message) []sdk.Message {
	return co.OptimizeMessagesWithModel(messages, "")
}

// OptimizeMessagesWithModel reduces token usage with optional current model for fallback
func (co *ConversationOptimizer) OptimizeMessagesWithModel(messages []sdk.Message, currentModel string) []sdk.Message {
	if !co.enabled || len(messages) < co.minMessages {
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

	if len(conversationMessages) >= co.minMessages {
		originalModel := co.model
		if co.model == "" && currentModel != "" {
			co.model = currentModel
		}

		optimized := co.smartOptimize(conversationMessages)
		conversationMessages = optimized

		co.model = originalModel
	}

	return append(systemMessages, conversationMessages...)
}

// smartOptimize implements the smart optimization strategy
// It keeps the first user message (root context), summarizes the middle, and preserves recent context
func (co *ConversationOptimizer) smartOptimize(messages []sdk.Message) []sdk.Message {
	if len(messages) < co.minMessages {
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

	summary := co.generateSmartSummary(messagesToSummarize)
	if summary != "" {
		summaryMsg := sdk.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("[Context Summary: %s]", summary),
		}
		result = append(result, summaryMsg)
	}

	result = append(result, messages[lastMessagesStart:]...)

	return result
}

// generateSmartSummary creates an intelligent summary using LLM
func (co *ConversationOptimizer) generateSmartSummary(messages []sdk.Message) string {
	if len(messages) == 0 {
		return ""
	}

	if co.client == nil || co.model == "" {
		return co.createBasicSummary(messages)
	}

	summary, err := co.generateLLMSummary(messages)
	if err != nil {
		logger.Debug("LLM summarization failed, falling back to basic summary", "error", err)
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
func (co *ConversationOptimizer) generateLLMSummary(messages []sdk.Message) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summaryMessages := make([]sdk.Message, 0, len(messages)+2)

	summaryMessages = append(summaryMessages, sdk.Message{
		Role: sdk.System,
		Content: `You are a conversation summarizer. Create a concise summary that preserves the essential context and progress made in the conversation.

Focus on:
- Key tasks completed or in progress
- Important decisions or findings
- Critical context needed to continue the conversation
- Any unresolved issues or next steps

Keep the summary brief but informative (2-3 sentences max).`,
	})

	for _, msg := range messages {
		switch msg.Role {
		case sdk.User, sdk.Assistant:
			content := msg.Content
			if len(content) > 2000 {
				content = content[:2000] + "... [truncated]"
			}

			summaryMessages = append(summaryMessages, sdk.Message{
				Role:    msg.Role,
				Content: content,
			})
		case "tool":
			content := msg.Content
			if len(content) > 500 {
				content = content[:500] + "... [tool output truncated]"
			}
			summaryMessages = append(summaryMessages, sdk.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("[Tool result: %s]", content),
			})
		}
	}

	summaryMessages = append(summaryMessages, sdk.Message{
		Role:    sdk.User,
		Content: "Provide a concise summary of the conversation above, focusing on key progress and context needed to continue.",
	})

	if co.model == "" {
		return "", fmt.Errorf("no model configured for summarization")
	}

	slashIndex := strings.Index(co.model, "/")
	if slashIndex == -1 {
		return "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}
	provider := co.model[:slashIndex]
	modelName := co.model[slashIndex+1:]

	maxTokens := 200
	options := &sdk.CreateChatCompletionRequest{
		MaxTokens: &maxTokens,
	}

	response, err := co.client.
		WithOptions(options).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
			SkipA2A: true,
		}).
		GenerateContent(ctx, sdk.Provider(provider), modelName, summaryMessages)

	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no summary generated")
	}

	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}
