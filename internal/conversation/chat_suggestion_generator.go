package conversation

import (
	"context"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// maxSuggestionChars is the one-line length cap for next-step suggestions.
const maxSuggestionChars = 80

// SuggestionTelemetryRecorder is the slice of the telemetry recorder the
// suggestion call needs to mirror judge usage accounting.
type SuggestionTelemetryRecorder interface {
	RecordUsage(model string, promptTokens, completionTokens, cachedTokens, cacheWriteTokens int)
}

// ChatSuggestionGenerator produces the one-line next-step suggestion shown as
// ghost text in the chat input after an assistant turn completes. Unlike the
// judge it is a convenience call with silent-fail semantics: every problem -
// disabled, misconfigured, failed request, non-conforming answer - yields no
// suggestion and logs at debug.
type ChatSuggestionGenerator struct {
	client   sdk.Client
	repo     convdomain.ConversationRepository
	config   *config.Config
	recorder SuggestionTelemetryRecorder
}

// NewChatSuggestionGenerator creates the generator. recorder is optional
// telemetry; the repo receives the call's tokens either way.
func NewChatSuggestionGenerator(client sdk.Client, repo convdomain.ConversationRepository, config *config.Config, recorder SuggestionTelemetryRecorder) *ChatSuggestionGenerator {
	return &ChatSuggestionGenerator{client: client, repo: repo, config: config, recorder: recorder}
}

// GenerateNextStepSuggestion asks the configured model for a one-line follow-up
// suggestion grounded in the most recent conversation messages. Returns "" for
// every failure shape and never surfaces an error.
func (g *ChatSuggestionGenerator) GenerateNextStepSuggestion(ctx context.Context) string {
	if g == nil || g.client == nil || g.repo == nil || g.config == nil || !g.config.Chat.Suggestions.Enabled {
		return ""
	}

	cfg := g.config.EffectiveSuggestions()
	provider, modelName := config.ParseModel(cfg.Model)
	if provider == "" {
		logger.Debug("suggestion: no usable chat.suggestions.model configured", "model", cfg.Model)
		return ""
	}

	dialog := g.suggestionDialog(cfg.ContextMessages)
	if dialog == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.Timeout)*time.Second)
	defer cancel()

	response, err := g.client.
		WithOptions(&sdk.CreateChatCompletionRequest{MaxTokens: &cfg.MaxTokens}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{SkipMCP: true}).
		GenerateContent(ctx, sdk.Provider(provider), modelName, g.suggestionMessages(dialog))
	if err != nil {
		logger.Debug("suggestion: request failed", "error", err)
		return ""
	}
	if len(response.Choices) == 0 {
		logger.Debug("suggestion: response had no choices")
		return ""
	}
	raw, err := response.Choices[0].Message.Content.AsMessageContent0()
	if err != nil {
		logger.Debug("suggestion: could not read response content", "error", err)
		return ""
	}

	g.recordUsage(cfg.Model, response.Usage)

	return SanitizeNextStepSuggestion(raw)
}

// recordUsage adds the suggestion call's tokens to the session totals (status
// bar, cost reports) and, when wired, the telemetry recorder.
func (g *ChatSuggestionGenerator) recordUsage(model string, usage *sdk.CompletionUsage) {
	if usage == nil {
		return
	}
	usageInput, usageOutput, usageTotal := int(usage.PromptTokens), int(usage.CompletionTokens), int(usage.TotalTokens)
	if err := g.repo.AddTokenUsage(model, usageInput, usageOutput, usageTotal, 0, 0); err != nil {
		logger.Debug("suggestion: failed to add token usage to session", "error", err)
	}
	if g.recorder != nil {
		g.recorder.RecordUsage(model, usageInput, usageOutput, 0, 0)
	}
}

// suggestionMessages builds the two-message payload: system contract plus the
// recent conversation transcript.
func (g *ChatSuggestionGenerator) suggestionMessages(dialog string) []sdk.Message {
	return []sdk.Message{
		{Role: sdk.System, Content: sdk.NewMessageContent(g.config.Prompts.Chat.Suggestions.SystemPrompt)},
		{Role: sdk.User, Content: sdk.NewMessageContent("Recent conversation:\n\n" + dialog)},
	}
}

// suggestionDialog renders the most recent non-hidden conversation entries -
// user and assistant turns plus condensed tool lines - as prompt text.
func (g *ChatSuggestionGenerator) suggestionDialog(contextMessages int) string {
	entries := g.repo.GetMessages()
	relevant := make([]convdomain.ConversationEntry, 0, contextMessages)
	for _, entry := range entries {
		if entry.Hidden {
			continue
		}
		relevant = append(relevant, entry)
	}
	if len(relevant) > contextMessages {
		relevant = relevant[len(relevant)-contextMessages:]
	}

	var dialog strings.Builder
	for _, entry := range relevant {
		line := suggestionLine(entry)
		if line == "" {
			continue
		}
		dialog.WriteString(line)
		dialog.WriteString("\n")
	}
	return strings.TrimSpace(dialog.String())
}

// suggestionLine summarises one entry to its labeled single line, or "" when
// the entry carries nothing readable.
func suggestionLine(entry convdomain.ConversationEntry) string {
	content, _ := entry.Message.Content.AsMessageContent0()
	content = strings.TrimSpace(content)
	switch entry.Message.Role {
	case sdk.User:
		return truncateSuggestionLine("User: "+content, 200)
	case sdk.Assistant:
		if names := toolCallNames(entry.Message.ToolCalls); len(names) > 0 {
			return truncateSuggestionLine("Assistant ran tools: "+strings.Join(names, ", "), 200)
		}
		return truncateSuggestionLine("Assistant: "+content, 200)
	case sdk.Tool:
		return truncateSuggestionLine("Tool result: "+content, 100)
	default:
		return ""
	}
}

// toolCallNames flattens an assistant message's tool calls to their names.
func toolCallNames(toolCalls *[]sdk.ChatCompletionMessageToolCall) []string {
	if toolCalls == nil {
		return nil
	}
	names := make([]string, 0, len(*toolCalls))
	for _, toolCall := range *toolCalls {
		names = append(names, toolCall.Function.Name)
	}
	return names
}

// truncateSuggestionLine caps a summary line, marking the cut.
func truncateSuggestionLine(line string, maxLen int) string {
	if len(line) <= maxLen {
		return line
	}
	return line[:maxLen] + "..."
}

// SanitizeNextStepSuggestion applies the suggestion output contract: one
// plain-text line, no surrounding quotes or code fences, at most
// maxSuggestionChars. Empty and multi-line answers are discarded.
func SanitizeNextStepSuggestion(raw string) string {
	suggestion := strings.TrimSpace(raw)
	suggestion = stripCodeFences(suggestion)
	suggestion = strings.TrimSpace(suggestion)
	suggestion = stripSurroundingQuotes(suggestion)
	if suggestion == "" || strings.Contains(suggestion, "\n") {
		return ""
	}
	if len(suggestion) <= maxSuggestionChars {
		return suggestion
	}

	if lastSpace := strings.LastIndex(suggestion[:maxSuggestionChars], " "); lastSpace > maxSuggestionChars/2 {
		return suggestion[:lastSpace]
	}
	return suggestion[:maxSuggestionChars]
}

// stripCodeFences drops a surrounding ``` block wrapper, keeping the body.
func stripCodeFences(raw string) string {
	lines := strings.Split(raw, "\n")
	if len(lines) < 2 || !strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		return raw
	}
	lines = lines[1:]
	if last := strings.TrimSpace(lines[len(lines)-1]); last == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// stripSurroundingQuotes removes paired wrapping quotes, longest-first.
func stripSurroundingQuotes(raw string) string {
	for len(raw) >= 2 {
		first, last := raw[0], raw[len(raw)-1]
		if (first != '"' && first != '\'' && first != '`') || first != last {
			return raw
		}
		raw = strings.TrimSpace(raw[1 : len(raw)-1])
	}
	return raw
}
