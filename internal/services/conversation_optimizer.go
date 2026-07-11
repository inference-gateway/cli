package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	logger "github.com/inference-gateway/cli/internal/logger"
	models "github.com/inference-gateway/cli/internal/models"
	streamevent "github.com/inference-gateway/cli/internal/streamevent"
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
	repo              domain.ConversationRepository
}

var _ domain.ConversationOptimizer = (*ConversationOptimizer)(nil)

// OptimizerConfig represents configuration for the conversation optimizer
type OptimizerConfig struct {
	Enabled           bool
	AutoAt            int
	BufferSize        int
	KeepFirstMessages int
	Client            sdk.Client
	Config            *config.Config
	Tokenizer         *TokenizerService
	// Repo is optional. When provided, OptimizeMessages reads
	// LastInputTokens from the repo's session stats and uses it as the
	// trigger value (the gateway-reported count includes system prompt and
	// tool definitions, matching what `/context` displays). When nil, the
	// gate falls back to the entries-only estimate.
	Repo domain.ConversationRepository
}

// NewConversationOptimizer creates a new conversation optimizer with configuration
func NewConversationOptimizer(config OptimizerConfig) domain.ConversationOptimizer {
	if config.AutoAt < 1 || config.AutoAt > 80 {
		logger.Warn("compact.auto_at must be between 1 and 80, defaulting to 80", "provided", config.AutoAt)
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
		repo:              config.Repo,
	}
}

// estimateTriggerTokens returns the value used to gate auto-compaction. It
// prefers the gateway-reported LastInputTokens (which includes system prompt
// and tool definitions) and falls back to the entries-only tokenizer estimate
// before the first round-trip or when no repo is wired in.
func (co *ConversationOptimizer) estimateTriggerTokens(messages []sdk.Message) int {
	if co.repo != nil {
		if stats := co.repo.GetSessionTokens(); stats.LastInputTokens > 0 {
			return stats.LastInputTokens
		}
	}
	return co.tokenizer.EstimateMessagesTokens(messages)
}

// OptimizeMessages reduces token usage by intelligently managing conversation
// history with LLM summarization. When force is false it is a no-op unless the
// model has a configured context window and the conversation has crossed
// auto_at percent of it; when force is true it always compacts (manual /compact
// and session rollover both rely on this).
func (co *ConversationOptimizer) OptimizeMessages(messages []sdk.Message, model string, force bool) []sdk.Message {
	if len(messages) == 0 {
		return messages
	}

	if !force && !co.enabled {
		return messages
	}

	contextWindow, known := models.LookupContextWindow(model)
	if !force && !known {
		return messages
	}

	threshold := (contextWindow * co.autoAt) / 100
	currentTokens := co.estimateTriggerTokens(messages)

	if !force && currentTokens < threshold {
		return messages
	}

	logger.Debug("conversation compaction triggered",
		"current_tokens", currentTokens,
		"threshold", threshold,
		"context_window", contextWindow,
		"auto_at_pct", co.autoAt,
		"messages_before", len(messages),
		"force", force,
	)
	streamevent.EmitDebugEvent("compaction_started", map[string]any{
		"current_tokens":  currentTokens,
		"threshold":       threshold,
		"context_window":  contextWindow,
		"auto_at_pct":     co.autoAt,
		"messages_before": len(messages),
		"force":           force,
	})

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
		logger.Error("optimization failed", "error", err)
		return messages
	}
	result := append(systemMessages, optimized...)
	streamevent.EmitDebugEvent("compaction_completed", map[string]any{
		"messages_before": len(messages),
		"messages_after":  len(result),
	})
	return result
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

	if summaryStartIndex < co.keepFirstMessages {
		logger.Info("adjusting kept messages due to boundary moved back",
			"original_keep", co.keepFirstMessages,
			"adjusted_keep", summaryStartIndex)
		result = make([]sdk.Message, summaryStartIndex)
		copy(result, messages[:summaryStartIndex])
	} else if summaryStartIndex > co.keepFirstMessages {
		logger.Info("adjusting kept messages due to boundary moved forward to include tool responses",
			"original_keep", co.keepFirstMessages,
			"adjusted_keep", summaryStartIndex)
		result = make([]sdk.Message, summaryStartIndex)
		copy(result, messages[:summaryStartIndex])
	}

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

	logger.Info("generating LLM summary for compaction", "model", model, "messages_to_summarize", len(messagesToSummarize))
	summary, err := co.GenerateLLMSummary(messagesToSummarize, model)
	if err != nil {
		logger.Error("failed to generate LLM summary", "error", err)
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	logger.Info("lLM summary generated successfully", "summary_length", len(summary))

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
// If tool calls cannot be resolved (interrupted by user/assistant messages), the
// boundary is moved back to exclude the assistant message with incomplete tool calls.
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
		if tc.ID != "" {
			toolCallIDs[tc.ID] = true
		}
	}

	adjustedBoundary := boundaryIndex
	for i := boundaryIndex; i < len(messages); i++ {
		if messages[i].Role == "tool" && messages[i].ToolCallID != nil {
			if toolCallIDs[*messages[i].ToolCallID] {
				adjustedBoundary = i + 1
				delete(toolCallIDs, *messages[i].ToolCallID)
			}
		} else if messages[i].Role == "assistant" || messages[i].Role == "user" {
			break
		}
	}

	if len(toolCallIDs) > 0 {
		logger.Warn("found unresolved tool calls at compaction boundary, moving boundary back",
			"count", len(toolCallIDs),
			"original_boundary", boundaryIndex,
			"attempted_boundary", adjustedBoundary)
		for i := boundaryIndex - 1; i >= 0; i-- {
			if messages[i].Role == "assistant" &&
				messages[i].ToolCalls != nil &&
				len(*messages[i].ToolCalls) > 0 {
				logger.Info("moving boundary back to exclude assistant message with incomplete tool calls",
					"new_boundary", i)
				return i
			}
		}

		if boundaryIndex > 1 {
			logger.Warn("could not find assistant message with tool calls, using boundary - 1",
				"new_boundary", boundaryIndex-1)
			return boundaryIndex - 1
		}
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

	maxTokens := 1024
	if co.config != nil && co.config.Compact.SummaryMaxTokens > 0 {
		maxTokens = co.config.Compact.SummaryMaxTokens
	}
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

	if response.Choices[0].FinishReason == sdk.Length {
		logger.Warn("summary truncated by max_tokens cap; consider raising compact.summary_max_tokens",
			"max_tokens", maxTokens,
			"summary_length", len(contentStr))
	}

	return strings.TrimSpace(contentStr), nil
}
