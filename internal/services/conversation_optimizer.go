package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/inference-gateway/sdk"
)

// ConversationOptimizer provides methods to optimize conversation history for token efficiency
type ConversationOptimizer struct {
	enabled                    bool
	maxHistorySize             int
	compactThreshold           int
	truncateLargeOutputs       bool
	skipRedundantConfirmations bool
	client                     sdk.Client
	modelService               domain.ModelService
	config                     *config.Config
}

// OptimizerConfig represents configuration for the conversation optimizer
type OptimizerConfig struct {
	Enabled                    bool
	MaxHistory                 int
	CompactThreshold           int
	TruncateLargeOutputs       bool
	SkipRedundantConfirmations bool
	Client                     sdk.Client
	ModelService               domain.ModelService
	Config                     *config.Config
}

// NewConversationOptimizer creates a new conversation optimizer with configuration
func NewConversationOptimizer(config OptimizerConfig) *ConversationOptimizer {
	if config.MaxHistory <= 0 {
		config.MaxHistory = 10
	}
	if config.CompactThreshold <= 0 {
		config.CompactThreshold = 20
	}

	return &ConversationOptimizer{
		enabled:                    config.Enabled,
		maxHistorySize:             config.MaxHistory,
		compactThreshold:           config.CompactThreshold,
		truncateLargeOutputs:       config.TruncateLargeOutputs,
		skipRedundantConfirmations: config.SkipRedundantConfirmations,
		client:                     config.Client,
		modelService:               config.ModelService,
		config:                     config.Config,
	}
}

// OptimizeMessages reduces token usage by intelligently managing conversation history
func (co *ConversationOptimizer) OptimizeMessages(messages []sdk.Message) []sdk.Message {
	if !co.enabled || len(messages) <= co.maxHistorySize {
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

	if len(conversationMessages) > co.maxHistorySize {
		optimized := co.preserveToolCallPairs(conversationMessages)
		conversationMessages = optimized
	}

	return append(systemMessages, conversationMessages...)
}

// preserveToolCallPairs ensures tool messages always follow their corresponding tool_calls
func (co *ConversationOptimizer) preserveToolCallPairs(messages []sdk.Message) []sdk.Message {
	if len(messages) <= co.maxHistorySize {
		return messages
	}

	result := []sdk.Message{}
	if len(messages) > 0 && messages[0].Role == "user" {
		result = append(result, messages[0])
	}

	recentCount := co.maxHistorySize - len(result) - 1
	if recentCount <= 0 {
		return result
	}

	cutoffIndex := len(messages) - recentCount

	for i := cutoffIndex; i < len(messages); i++ {
		if messages[i].Role == "tool" {
			for j := i - 1; j >= 0; j-- {
				if messages[j].Role == "assistant" && messages[j].ToolCalls != nil && len(*messages[j].ToolCalls) > 0 {
					cutoffIndex = j
					break
				}
			}
			break
		}
	}

	if cutoffIndex > 1 {
		summary := co.createHistorySummary(messages[1:cutoffIndex])
		if summary != "" {
			summaryMsg := sdk.Message{
				Role:    "assistant",
				Content: summary,
			}
			result = append(result, summaryMsg)
		}
	}

	result = append(result, messages[cutoffIndex:]...)

	return result
}

// createHistorySummary creates a summary of messages that will be omitted using LLM
func (co *ConversationOptimizer) createHistorySummary(messages []sdk.Message) string {
	if len(messages) == 0 {
		return ""
	}

	if co.client != nil && co.modelService != nil && co.config != nil {
		if summary, err := co.generateLLMSummary(messages); err == nil && summary != "" {
			return fmt.Sprintf("[Previous conversation summary: %s]", strings.TrimSpace(summary))
		} else {
			logger.Debug("LLM summarization failed, falling back to basic summary", "error", err)
		}
	}

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

	return fmt.Sprintf("[Previous conversation context: %d user messages, %d assistant responses, %d tool executions. The conversation history has been optimized to reduce token usage while preserving recent context.]",
		userCount, assistantCount, toolCount)
}

// generateLLMSummary uses the SDK client to generate an intelligent summary
func (co *ConversationOptimizer) generateLLMSummary(messages []sdk.Message) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summaryMessages := make([]sdk.Message, 0, len(messages)+2)

	summaryMessages = append(summaryMessages, sdk.Message{
		Role: sdk.System,
		Content: `You are a helpful assistant that creates very concise summaries of chat conversations for token optimization. Create a brief 1-2 sentence summary that captures:
- Main topics or tasks discussed
- Key decisions or conclusions reached
- Current context that would be important for continuing the conversation

Keep it extremely concise - this summary will be inserted into an ongoing conversation to preserve context.`,
	})

	for _, msg := range messages {
		if msg.Role == sdk.User || msg.Role == sdk.Assistant {
			content := msg.Content
			if len(content) > 2000 {
				content = content[:2000] + "... [truncated]"
			}

			summaryMessages = append(summaryMessages, sdk.Message{
				Role:    msg.Role,
				Content: content,
			})
		}
	}

	summaryMessages = append(summaryMessages, sdk.Message{
		Role:    sdk.User,
		Content: "Please provide a very brief summary of the above conversation in 1-2 sentences.",
	})

	summaryModel := ""
	if co.config.Compact.SummaryModel != "" {
		summaryModel = co.config.Compact.SummaryModel
	} else {
		summaryModel = co.modelService.GetCurrentModel()
	}

	if summaryModel == "" {
		return "", fmt.Errorf("no model available for summary generation")
	}

	slashIndex := strings.Index(summaryModel, "/")
	if slashIndex == -1 {
		return "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}
	provider := summaryModel[:slashIndex]
	modelName := summaryModel[slashIndex+1:]

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

// CompactToolCalls reduces the verbosity of tool call results
func (co *ConversationOptimizer) CompactToolCalls(message sdk.Message) sdk.Message {
	if !co.enabled || !co.truncateLargeOutputs || message.Role != "tool" || len(message.Content) < 1000 {
		return message
	}

	lines := strings.Split(message.Content, "\n")
	if len(lines) > 50 {
		compacted := append(lines[:20], fmt.Sprintf("\n... (%d lines omitted) ...\n", len(lines)-30))
		compacted = append(compacted, lines[len(lines)-10:]...)
		message.Content = strings.Join(compacted, "\n")
	}

	return message
}

// SummarizeOldMessages creates a summary of older messages to reduce tokens
func (co *ConversationOptimizer) SummarizeOldMessages(messages []domain.ConversationEntry) string {
	if !co.enabled || len(messages) <= co.compactThreshold {
		return ""
	}

	oldMessages := messages[:len(messages)-co.compactThreshold]
	userCount := 0
	assistantCount := 0
	toolCount := 0

	for _, msg := range oldMessages {
		switch msg.Message.Role {
		case "user":
			userCount++
		case "assistant":
			assistantCount++
		case "tool":
			toolCount++
		}
	}

	return fmt.Sprintf("[Previous context: %d user messages, %d assistant responses, %d tool executions]",
		userCount, assistantCount, toolCount)
}

// OptimizeForExport prepares messages for export with token optimization
func (co *ConversationOptimizer) OptimizeForExport(entries []domain.ConversationEntry) []domain.ConversationEntry {
	if !co.enabled {
		return entries
	}

	optimized := make([]domain.ConversationEntry, 0, len(entries))

	for _, entry := range entries {
		if co.skipRedundantConfirmations && entry.Message.Role == "assistant" && strings.Contains(entry.Message.Content, "I'll use the") {
			continue
		}

		if co.truncateLargeOutputs && len(entry.Message.Content) > 5000 {
			entry.Message.Content = entry.Message.Content[:2000] + "\n\n[... content truncated ...]\n\n" + entry.Message.Content[len(entry.Message.Content)-1000:]
		}

		optimized = append(optimized, entry)
	}

	return optimized
}

// EstimateTokens provides rough token estimation (4 chars ≈ 1 token)
func (co *ConversationOptimizer) EstimateTokens(content string) int {
	return len(content) / 4
}

// GetOptimizationStats returns statistics about potential token savings
func (co *ConversationOptimizer) GetOptimizationStats(messages []sdk.Message) map[string]int {
	originalTokens := 0
	optimizedTokens := 0

	for _, msg := range messages {
		originalTokens += co.EstimateTokens(msg.Content)
		if msg.ToolCalls != nil {
			for _, tc := range *msg.ToolCalls {
				originalTokens += co.EstimateTokens(tc.Function.Arguments)
			}
		}
	}

	optimized := co.OptimizeMessages(messages)
	for _, msg := range optimized {
		optimizedTokens += co.EstimateTokens(msg.Content)
		if msg.ToolCalls != nil {
			for _, tc := range *msg.ToolCalls {
				optimizedTokens += co.EstimateTokens(tc.Function.Arguments)
			}
		}
	}

	return map[string]int{
		"original_tokens":  originalTokens,
		"optimized_tokens": optimizedTokens,
		"saved_tokens":     originalTokens - optimizedTokens,
		"saved_percent":    (originalTokens - optimizedTokens) * 100 / originalTokens,
	}
}
