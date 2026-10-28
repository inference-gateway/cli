package conversation

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	models "github.com/inference-gateway/cli/internal/platform/models"
	streamevent "github.com/inference-gateway/cli/internal/platform/streamevent"
)

// ConversationOptimizer provides methods to optimize conversation history for token efficiency
type ConversationOptimizer struct {
	enabled           bool
	autoAt            int
	bufferSize        int
	keepFirstMessages int
	strategy          config.CompactStrategy
	client            sdk.Client
	config            *config.Config
	tokenizer         *TokenizerService
	repo              convdomain.ConversationRepository
	restore           func(summarized []sdk.Message) []sdk.Message
	skipAutoChecks    int
}

var _ convdomain.ConversationOptimizer = (*ConversationOptimizer)(nil)

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
	Repo convdomain.ConversationRepository
	// RestoreContext is optional. It receives the messages that were folded
	// into the summary and returns messages to append after it, so context
	// the agent must keep verbatim (loaded skills) survives compaction.
	RestoreContext func(summarized []sdk.Message) []sdk.Message
	Strategy       config.CompactStrategy
}

// NewConversationOptimizer creates a new conversation optimizer with configuration
func NewConversationOptimizer(cfg OptimizerConfig) convdomain.ConversationOptimizer {
	if cfg.AutoAt < 1 || cfg.AutoAt > 80 {
		logger.Warn("compact.auto_at must be between 1 and 80, defaulting to 80", "provided", cfg.AutoAt)
		cfg.AutoAt = 80
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 2
	}
	if cfg.KeepFirstMessages <= 0 {
		cfg.KeepFirstMessages = 2
	}
	if cfg.Strategy == "" {
		cfg.Strategy = config.CompactStrategySummary
	}
	if cfg.Strategy != config.CompactStrategySummary && cfg.Strategy != config.CompactStrategyPointer {
		logger.Warn("unknown compact.strategy, falling back to summary", "provided", cfg.Strategy)
		cfg.Strategy = config.CompactStrategySummary
	}

	tokenizer := cfg.Tokenizer
	if tokenizer == nil {
		tokenizer = NewTokenizerService(DefaultTokenizerConfig())
	}

	return &ConversationOptimizer{
		enabled:           cfg.Enabled,
		autoAt:            cfg.AutoAt,
		bufferSize:        cfg.BufferSize,
		keepFirstMessages: cfg.KeepFirstMessages,
		strategy:          cfg.Strategy,
		client:            cfg.Client,
		config:            cfg.Config,
		tokenizer:         tokenizer,
		repo:              cfg.Repo,
		restore:           cfg.RestoreContext,
	}
}

// estimateTriggerTokens returns the value used to gate auto-compaction: the
// larger of the gateway-reported LastInputTokens (which includes system prompt
// and tool definitions) and a fresh tokenizer estimate of the current buffer.
// Taking the max catches a single-turn tool-output spike that a stale
// LastInputTokens alone would miss.
func (co *ConversationOptimizer) estimateTriggerTokens(messages []sdk.Message) int {
	lastInput := 0
	if co.repo != nil {
		lastInput = co.repo.GetSessionTokens().LastInputTokens
	}
	return co.tokenizer.EffectiveContextTokens(lastInput, messages)
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

	if !force && (currentTokens < threshold || co.inBackoff()) {
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
		co.skipAutoChecks = co.bufferSize
		streamevent.EmitDebugEvent("compaction_failed", map[string]any{
			"error":           err.Error(),
			"messages_before": len(messages),
		})
		return messages
	}
	co.skipAutoChecks = 0
	result := append(systemMessages, optimized...)
	streamevent.EmitDebugEvent("compaction_completed", map[string]any{
		"messages_before": len(messages),
		"messages_after":  len(result),
	})
	return result
}

// contextSummaryMarker opens the checkpoint message a compaction produces.
const contextSummaryMarker = "--- Context Summary ---"

// smartOptimize keeps the first N messages (default 2) when they open with a
// user turn, folds the rest into one user-role checkpoint (summary or pointer
// strategy), and appends whatever the restore hook wants carried over verbatim.
func (co *ConversationOptimizer) smartOptimize(messages []sdk.Message, model string) ([]sdk.Message, error) {
	keep := co.headToKeep(messages)
	minMessages := keep + 1
	if len(messages) < minMessages {
		return messages, nil
	}

	result := make([]sdk.Message, keep)
	copy(result, messages[:keep])

	summaryStartIndex := keep

	summaryStartIndex = co.adjustBoundaryForToolCallsAtStart(messages, summaryStartIndex)

	if summaryStartIndex < keep {
		logger.Info("adjusting kept messages due to boundary moved back",
			"original_keep", keep,
			"adjusted_keep", summaryStartIndex)
		result = make([]sdk.Message, summaryStartIndex)
		copy(result, messages[:summaryStartIndex])
	} else if summaryStartIndex > keep {
		logger.Info("adjusting kept messages due to boundary moved forward to include tool responses",
			"original_keep", keep,
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

	checkpoint, err := co.checkpointMessage(messagesToSummarize, model)
	if err != nil {
		return nil, err
	}
	result = append(result, checkpoint)

	if co.restore != nil {
		result = append(result, co.restore(messagesToSummarize)...)
	}

	return result, nil
}

// checkpointMessage builds the single user-role checkpoint the continued
// session is seeded with. The summary strategy has the LLM write it, the
// pointer strategy skips that call and points at the previous session instead.
func (co *ConversationOptimizer) checkpointMessage(messages []sdk.Message, model string) (sdk.Message, error) {
	if co.strategy == config.CompactStrategyPointer {
		return co.pointerCheckpoint()
	}
	return co.summaryCheckpoint(messages, model)
}

// summaryCheckpoint summarizes the tail into the checkpoint message via an
// extra LLM call. The error makes the caller keep the history rather than
// drop it without a summary.
func (co *ConversationOptimizer) summaryCheckpoint(messages []sdk.Message, model string) (sdk.Message, error) {
	if co.client == nil {
		return sdk.Message{}, fmt.Errorf("LLM client is required for conversation compaction")
	}
	if model == "" {
		return sdk.Message{}, fmt.Errorf("model is required for conversation compaction")
	}

	logger.Info("generating LLM summary for compaction", "model", model, "messages_to_summarize", len(messages))
	summary, err := co.GenerateLLMSummary(messages, model)
	if err != nil {
		logger.Error("failed to generate LLM summary", "error", err)
		return sdk.Message{}, fmt.Errorf("failed to generate summary: %w", err)
	}

	logger.Info("lLM summary generated successfully", "summary_length", len(summary))

	wrappedSummary := formatting.WrapText(summary, 80)
	return sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(fmt.Sprintf("%s\n\n%s\n\n--- End Summary ---", contextSummaryMarker, wrappedSummary)),
	}, nil
}

// pointerCheckpoint seeds one message that says the replaced tail is still
// readable in the previous session, so the model pulls only what it needs.
// The id is read at compaction time - during a rollover, before
// StartNewConversation repoints the repo, it is still the previous session's id.
func (co *ConversationOptimizer) pointerCheckpoint() (sdk.Message, error) {
	previousID := ""
	if co.repo != nil {
		previousID = co.repo.GetCurrentConversationID()
	}
	if previousID == "" {
		return sdk.Message{}, fmt.Errorf("pointer compaction needs the current session id to point back at")
	}

	return sdk.Message{
		Role: sdk.User,
		Content: sdk.NewMessageContent(fmt.Sprintf(
			"%s\n\nThe earlier part of this conversation, including tool results, is stored in session `%s`. To look anything up, cherry-pick only the specific entries you need with `infer conversations show %s --grep <substring>` - do not load the whole session.",
			contextSummaryMarker, previousID, previousID,
		)),
	}, nil
}

// headToKeep returns how many leading messages to keep verbatim. A head that
// does not open with a user turn (the first entries were hidden) would be a
// run of orphaned tool calls, so nothing is kept and the checkpoint opens the
// conversation instead.
func (co *ConversationOptimizer) headToKeep(messages []sdk.Message) int {
	if len(messages) == 0 || messages[0].Role != sdk.User {
		return 0
	}
	return co.keepFirstMessages
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

// summarySystemPrompt is the fixed checkpoint template the summarizer fills.
// Section headings and the preservation rules follow what long-session agents
// converge on: terse bullets, exact identifiers, only still-open requests.
const summarySystemPrompt = `You are writing a context checkpoint for an agent that will continue this conversation with only your summary as history. Fill in every section below using exactly these Markdown headings.

## Objective
What the user is trying to accomplish.

## Important Details
Constraints, decisions, assumptions and context needed to continue.

## Work State
Completed work, active work and blockers.

## Next Move
The next concrete action to take.

## Relevant Files
File paths that are easy to lose.

Rules:
- Keep each section concise. Use terse, single-line bullets, not prose paragraphs or nested lists.
- Preserve exact file paths, symbols, commands, error strings, URLs and identifiers.
- Carry forward only user questions or requests that remain unanswered or require further action, in their exact wording.
- Preserve workflow state such as whether changes are uncommitted, committed, pushed, under review or merged.
- Do not mention the summary process or that context was compacted.`

// GenerateLLMSummary asks the model for a structured checkpoint of messages.
// An empty reply is an error so the caller keeps the original messages rather
// than dropping history without a summary.
func (co *ConversationOptimizer) GenerateLLMSummary(messages []sdk.Message, model string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	summaryMessages := make([]sdk.Message, 0, len(messages)+2)

	summaryMessages = append(summaryMessages, sdk.Message{
		Role:    sdk.System,
		Content: sdk.NewMessageContent(summarySystemPrompt),
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
		Content: sdk.NewMessageContent("Write the context checkpoint for the conversation above using the required section headings."),
	})

	if model == "" {
		return "", fmt.Errorf("no model configured for summarization")
	}

	provider, modelName, ok := strings.Cut(model, "/")
	if !ok {
		return "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

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

	summary := strings.TrimSpace(contentStr)
	if summary == "" {
		return "", fmt.Errorf("summary generation returned no text")
	}

	return summary, nil
}

// inBackoff reports whether auto-compaction should wait after a failed summary:
// retrying on the very next turn with the same context would fail the same way
// and cost a summarisation request each time, so it skips bufferSize turns.
func (co *ConversationOptimizer) inBackoff() bool {
	if co.skipAutoChecks == 0 {
		return false
	}
	co.skipAutoChecks--
	return true
}
