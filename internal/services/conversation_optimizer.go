package services

import (
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/sdk"
)

// ConversationOptimizer provides methods to optimize conversation history for token efficiency
type ConversationOptimizer struct {
	enabled                    bool
	maxHistorySize             int
	compactThreshold           int
	truncateLargeOutputs       bool
	skipRedundantConfirmations bool
}

// OptimizerConfig represents configuration for the conversation optimizer
type OptimizerConfig struct {
	Enabled                    bool
	MaxHistory                 int
	CompactThreshold           int
	TruncateLargeOutputs       bool
	SkipRedundantConfirmations bool
}

// NewConversationOptimizer creates a new conversation optimizer with configuration
func NewConversationOptimizer(config OptimizerConfig) *ConversationOptimizer {
	// Set defaults if not provided
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
	}
}

// OptimizeMessages reduces token usage by intelligently managing conversation history
func (co *ConversationOptimizer) OptimizeMessages(messages []sdk.Message) []sdk.Message {
	if !co.enabled || len(messages) <= co.maxHistorySize {
		return messages
	}

	// Keep system messages intact
	var systemMessages []sdk.Message
	var conversationMessages []sdk.Message

	for _, msg := range messages {
		if msg.Role == "system" {
			systemMessages = append(systemMessages, msg)
		} else {
			conversationMessages = append(conversationMessages, msg)
		}
	}

	// Apply sliding window to conversation messages
	if len(conversationMessages) > co.maxHistorySize {
		// Keep first message for context, then recent messages
		optimized := []sdk.Message{conversationMessages[0]}
		recentStart := len(conversationMessages) - co.maxHistorySize + 1
		optimized = append(optimized, conversationMessages[recentStart:]...)
		conversationMessages = optimized
	}

	// Combine and return
	return append(systemMessages, conversationMessages...)
}

// CompactToolCalls reduces the verbosity of tool call results
func (co *ConversationOptimizer) CompactToolCalls(message sdk.Message) sdk.Message {
	if !co.enabled || !co.truncateLargeOutputs || message.Role != "tool" || len(message.Content) < 1000 {
		return message
	}

	// For large tool results, summarize
	lines := strings.Split(message.Content, "\n")
	if len(lines) > 50 {
		// Keep first 20 and last 10 lines
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

	// Count message types in older messages
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
		// Skip redundant tool confirmations
		if co.skipRedundantConfirmations && entry.Message.Role == "assistant" && strings.Contains(entry.Message.Content, "I'll use the") {
			continue
		}

		// Compact large outputs
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
