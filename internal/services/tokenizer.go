package services

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/inference-gateway/sdk"
)

// TokenizerService provides token counting functionality for LLM messages.
// This is a polyfill for providers (like Ollama Cloud) that don't return
// token usage metrics in their API responses.
type TokenizerService struct {
	// charsPerToken is the average characters per token estimate
	// OpenAI suggests ~4 characters per token for English text
	charsPerToken float64

	// messageOverhead is the estimated token overhead per message
	// for role markers, separators, and formatting
	messageOverhead int

	// toolCallOverhead is the estimated additional tokens for tool call formatting
	toolCallOverhead int
}

// TokenizerConfig holds configuration for the tokenizer service
type TokenizerConfig struct {
	// CharsPerToken is the average characters per token (default: 4.0)
	CharsPerToken float64

	// MessageOverhead is tokens per message for formatting (default: 4)
	MessageOverhead int

	// ToolCallOverhead is extra tokens per tool call (default: 10)
	ToolCallOverhead int
}

// DefaultTokenizerConfig returns the default tokenizer configuration
func DefaultTokenizerConfig() TokenizerConfig {
	return TokenizerConfig{
		CharsPerToken:    4.0,
		MessageOverhead:  4,
		ToolCallOverhead: 10,
	}
}

// NewTokenizerService creates a new tokenizer service with the given configuration
func NewTokenizerService(config TokenizerConfig) *TokenizerService {
	if config.CharsPerToken <= 0 {
		config.CharsPerToken = 4.0
	}
	if config.MessageOverhead <= 0 {
		config.MessageOverhead = 4
	}
	if config.ToolCallOverhead <= 0 {
		config.ToolCallOverhead = 10
	}

	return &TokenizerService{
		charsPerToken:    config.CharsPerToken,
		messageOverhead:  config.MessageOverhead,
		toolCallOverhead: config.ToolCallOverhead,
	}
}

// EstimateTokenCount estimates the number of tokens in a text string.
// This uses a character-based heuristic that provides a reasonable
// approximation for most English text.
func (t *TokenizerService) EstimateTokenCount(text string) int {
	if text == "" {
		return 0
	}

	// Use rune count for proper Unicode handling
	charCount := utf8.RuneCountInString(text)

	// Calculate token estimate
	tokens := float64(charCount) / t.charsPerToken

	// Round up to ensure we don't underestimate
	return int(tokens + 0.5)
}

// EstimateMessageTokens estimates the total tokens for a single message
func (t *TokenizerService) EstimateMessageTokens(msg sdk.Message) int {
	tokens := t.messageOverhead

	// Count content tokens
	contentStr, err := msg.Content.AsMessageContent0()
	if err == nil && contentStr != "" {
		tokens += t.EstimateTokenCount(contentStr)
	}

	// Add role token (typically 1-2 tokens)
	tokens += t.EstimateTokenCount(string(msg.Role))

	// Add tokens for tool calls if present
	if msg.ToolCalls != nil {
		for _, tc := range *msg.ToolCalls {
			tokens += t.toolCallOverhead
			tokens += t.EstimateTokenCount(tc.Function.Name)
			tokens += t.EstimateTokenCount(tc.Function.Arguments)
		}
	}

	// Add tokens for tool call ID if present
	if msg.ToolCallId != nil {
		tokens += t.EstimateTokenCount(*msg.ToolCallId)
	}

	// Add tokens for reasoning if present
	if msg.Reasoning != nil {
		tokens += t.EstimateTokenCount(*msg.Reasoning)
	}
	if msg.ReasoningContent != nil {
		tokens += t.EstimateTokenCount(*msg.ReasoningContent)
	}

	return tokens
}

// EstimateMessagesTokens estimates the total tokens for a slice of messages.
// This is useful for estimating the prompt/input token count.
func (t *TokenizerService) EstimateMessagesTokens(messages []sdk.Message) int {
	if len(messages) == 0 {
		return 0
	}

	totalTokens := 0
	for _, msg := range messages {
		totalTokens += t.EstimateMessageTokens(msg)
	}

	// Add base overhead for the messages array structure
	// (typically 3-4 tokens for message list formatting)
	totalTokens += 3

	return totalTokens
}

// EstimateToolDefinitionsTokens estimates tokens for tool definitions
func (t *TokenizerService) EstimateToolDefinitionsTokens(tools []sdk.ChatCompletionTool) int {
	if len(tools) == 0 {
		return 0
	}

	totalTokens := 0
	for _, tool := range tools {
		// Add tokens for function name and description
		totalTokens += t.EstimateTokenCount(tool.Function.Name)
		if tool.Function.Description != nil {
			totalTokens += t.EstimateTokenCount(*tool.Function.Description)
		}

		// Estimate tokens for the parameters schema
		if tool.Function.Parameters != nil {
			paramsJSON, err := json.Marshal(tool.Function.Parameters)
			if err == nil {
				totalTokens += t.EstimateTokenCount(string(paramsJSON))
			}
		}

		// Add overhead for tool structure
		totalTokens += t.toolCallOverhead
	}

	return totalTokens
}

// EstimateResponseTokens estimates the tokens in an LLM response string
func (t *TokenizerService) EstimateResponseTokens(response string) int {
	if response == "" {
		return 0
	}

	tokens := t.EstimateTokenCount(response)

	// Add message overhead for the assistant response
	tokens += t.messageOverhead

	return tokens
}

// CalculateUsagePolyfill creates a CompletionUsage estimate for providers
// that don't return usage metrics. This is the main entry point for the polyfill.
func (t *TokenizerService) CalculateUsagePolyfill(
	inputMessages []sdk.Message,
	outputContent string,
	outputToolCalls []sdk.ChatCompletionMessageToolCall,
	tools []sdk.ChatCompletionTool,
) *sdk.CompletionUsage {
	// Calculate prompt tokens (input)
	promptTokens := t.EstimateMessagesTokens(inputMessages)

	// Add tool definitions if present
	if len(tools) > 0 {
		promptTokens += t.EstimateToolDefinitionsTokens(tools)
	}

	// Calculate completion tokens (output)
	completionTokens := t.EstimateResponseTokens(outputContent)

	// Add tokens for tool calls in the response
	for _, tc := range outputToolCalls {
		completionTokens += t.toolCallOverhead
		completionTokens += t.EstimateTokenCount(tc.Function.Name)
		completionTokens += t.EstimateTokenCount(tc.Function.Arguments)
	}

	totalTokens := promptTokens + completionTokens

	return &sdk.CompletionUsage{
		PromptTokens:     int64(promptTokens),
		CompletionTokens: int64(completionTokens),
		TotalTokens:      int64(totalTokens),
	}
}

// ShouldUsePolyfill determines if token estimation should be used
// based on whether the provider returned valid usage metrics
func (t *TokenizerService) ShouldUsePolyfill(usage *sdk.CompletionUsage) bool {
	if usage == nil {
		return true
	}

	// Some providers return zeros instead of nil
	return usage.TotalTokens == 0 && usage.PromptTokens == 0 && usage.CompletionTokens == 0
}

// IsLikelyCodeContent checks if the text appears to be code
// Code typically has a higher tokens-per-character ratio
func (t *TokenizerService) IsLikelyCodeContent(text string) bool {
	codeIndicators := []string{
		"func ", "def ", "class ", "import ", "package ",
		"const ", "var ", "let ", "return ", "if ", "for ",
		"while ", "switch ", "case ", "struct ", "interface ",
		"public ", "private ", "protected ", "static ",
		"<div", "<span", "<script", "<!--", "-->",
		"{", "}", "[", "]", "()", "=>", "->",
	}

	lowerText := strings.ToLower(text)
	codeScore := 0

	for _, indicator := range codeIndicators {
		if strings.Contains(lowerText, indicator) {
			codeScore++
		}
	}

	// If we find multiple code indicators, it's likely code
	return codeScore >= 3
}

// AdjustedEstimate provides a more accurate estimate for code vs prose
func (t *TokenizerService) AdjustedEstimate(text string) int {
	baseEstimate := t.EstimateTokenCount(text)

	// Code typically uses more tokens due to special characters and formatting
	if t.IsLikelyCodeContent(text) {
		// Increase estimate by ~25% for code content
		return int(float64(baseEstimate) * 1.25)
	}

	return baseEstimate
}
