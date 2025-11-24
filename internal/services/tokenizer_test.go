package services

import (
	"testing"

	"github.com/inference-gateway/sdk"
)

func TestNewTokenizerService(t *testing.T) {
	t.Run("uses default config values", func(t *testing.T) {
		config := DefaultTokenizerConfig()
		tokenizer := NewTokenizerService(config)

		if tokenizer.charsPerToken != 4.0 {
			t.Errorf("Expected charsPerToken 4.0, got %f", tokenizer.charsPerToken)
		}
		if tokenizer.messageOverhead != 4 {
			t.Errorf("Expected messageOverhead 4, got %d", tokenizer.messageOverhead)
		}
		if tokenizer.toolCallOverhead != 10 {
			t.Errorf("Expected toolCallOverhead 10, got %d", tokenizer.toolCallOverhead)
		}
	})

	t.Run("corrects invalid config values", func(t *testing.T) {
		config := TokenizerConfig{
			CharsPerToken:    0,
			MessageOverhead:  -1,
			ToolCallOverhead: 0,
		}
		tokenizer := NewTokenizerService(config)

		if tokenizer.charsPerToken != 4.0 {
			t.Errorf("Expected charsPerToken to be corrected to 4.0, got %f", tokenizer.charsPerToken)
		}
		if tokenizer.messageOverhead != 4 {
			t.Errorf("Expected messageOverhead to be corrected to 4, got %d", tokenizer.messageOverhead)
		}
		if tokenizer.toolCallOverhead != 10 {
			t.Errorf("Expected toolCallOverhead to be corrected to 10, got %d", tokenizer.toolCallOverhead)
		}
	})
}

func TestEstimateTokenCount(t *testing.T) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	tests := []struct {
		name      string
		text      string
		minTokens int
		maxTokens int
	}{
		{
			name:      "empty string",
			text:      "",
			minTokens: 0,
			maxTokens: 0,
		},
		{
			name:      "single word",
			text:      "hello",
			minTokens: 1,
			maxTokens: 3,
		},
		{
			name:      "short sentence",
			text:      "Hello, how are you?",
			minTokens: 3,
			maxTokens: 8,
		},
		{
			name:      "longer text",
			text:      "The quick brown fox jumps over the lazy dog. This is a common pangram used for testing.",
			minTokens: 15,
			maxTokens: 30,
		},
		{
			name:      "unicode text",
			text:      "こんにちは世界",
			minTokens: 1,
			maxTokens: 10,
		},
		{
			name:      "code snippet",
			text:      `func main() { fmt.Println("Hello, World!") }`,
			minTokens: 8,
			maxTokens: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenizer.EstimateTokenCount(tt.text)
			if result < tt.minTokens || result > tt.maxTokens {
				t.Errorf("EstimateTokenCount(%q) = %d, expected between %d and %d",
					tt.text, result, tt.minTokens, tt.maxTokens)
			}
		})
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	t.Run("simple user message", func(t *testing.T) {
		msg := sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent("Hello, how are you?"),
		}

		result := tokenizer.EstimateMessageTokens(msg)
		// Should be at least the overhead (4) + some content tokens
		if result < 5 {
			t.Errorf("Expected at least 5 tokens, got %d", result)
		}
	})

	t.Run("assistant message with tool calls", func(t *testing.T) {
		toolCalls := []sdk.ChatCompletionMessageToolCall{
			{
				Id:   "call_123",
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path": "/tmp/test.txt"}`,
				},
			},
		}

		msg := sdk.Message{
			Role:      sdk.Assistant,
			Content:   sdk.NewMessageContent("I'll read that file for you."),
			ToolCalls: &toolCalls,
		}

		result := tokenizer.EstimateMessageTokens(msg)
		// Should include message overhead + content + tool call overhead
		if result < 20 {
			t.Errorf("Expected at least 20 tokens for message with tool call, got %d", result)
		}
	})

	t.Run("tool result message", func(t *testing.T) {
		toolCallId := "call_123"
		msg := sdk.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent("File contents: test data"),
			ToolCallId: &toolCallId,
		}

		result := tokenizer.EstimateMessageTokens(msg)
		if result < 5 {
			t.Errorf("Expected at least 5 tokens, got %d", result)
		}
	})
}

func TestEstimateMessagesTokens(t *testing.T) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	t.Run("empty messages", func(t *testing.T) {
		result := tokenizer.EstimateMessagesTokens([]sdk.Message{})
		if result != 0 {
			t.Errorf("Expected 0 tokens for empty messages, got %d", result)
		}
	})

	t.Run("conversation with multiple messages", func(t *testing.T) {
		messages := []sdk.Message{
			{
				Role:    sdk.System,
				Content: sdk.NewMessageContent("You are a helpful assistant."),
			},
			{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("What is the capital of France?"),
			},
			{
				Role:    sdk.Assistant,
				Content: sdk.NewMessageContent("The capital of France is Paris."),
			},
		}

		result := tokenizer.EstimateMessagesTokens(messages)
		// Should be sum of all message tokens plus overhead
		if result < 20 {
			t.Errorf("Expected at least 20 tokens for conversation, got %d", result)
		}
	})
}

func TestCalculateUsagePolyfill(t *testing.T) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	t.Run("simple request/response", func(t *testing.T) {
		inputMessages := []sdk.Message{
			{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Hello!"),
			},
		}

		usage := tokenizer.CalculateUsagePolyfill(
			inputMessages,
			"Hi there! How can I help you?",
			nil,
			nil,
		)

		if usage == nil {
			t.Fatal("Expected non-nil usage")
		}
		if usage.PromptTokens <= 0 {
			t.Errorf("Expected positive PromptTokens, got %d", usage.PromptTokens)
		}
		if usage.CompletionTokens <= 0 {
			t.Errorf("Expected positive CompletionTokens, got %d", usage.CompletionTokens)
		}
		if usage.TotalTokens != usage.PromptTokens+usage.CompletionTokens {
			t.Errorf("TotalTokens (%d) should equal PromptTokens (%d) + CompletionTokens (%d)",
				usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens)
		}
	})

	t.Run("request with tool calls", func(t *testing.T) {
		inputMessages := []sdk.Message{
			{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Read the file /tmp/test.txt"),
			},
		}

		outputToolCalls := []sdk.ChatCompletionMessageToolCall{
			{
				Id:   "call_123",
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path": "/tmp/test.txt"}`,
				},
			},
		}

		usage := tokenizer.CalculateUsagePolyfill(
			inputMessages,
			"I'll read that file for you.",
			outputToolCalls,
			nil,
		)

		if usage == nil {
			t.Fatal("Expected non-nil usage")
		}
		// Tool calls should add to completion tokens
		if usage.CompletionTokens < 15 {
			t.Errorf("Expected more CompletionTokens with tool calls, got %d", usage.CompletionTokens)
		}
	})

	t.Run("request with tool definitions", func(t *testing.T) {
		inputMessages := []sdk.Message{
			{
				Role:    sdk.User,
				Content: sdk.NewMessageContent("Hello"),
			},
		}

		description := "Read the contents of a file"
		tools := []sdk.ChatCompletionTool{
			{
				Type: sdk.Function,
				Function: sdk.FunctionObject{
					Name:        "read_file",
					Description: &description,
					Parameters: &sdk.FunctionParameters{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "The file path to read",
							},
						},
						"required": []string{"path"},
					},
				},
			},
		}

		usageWithTools := tokenizer.CalculateUsagePolyfill(
			inputMessages,
			"Hello!",
			nil,
			tools,
		)

		usageWithoutTools := tokenizer.CalculateUsagePolyfill(
			inputMessages,
			"Hello!",
			nil,
			nil,
		)

		if usageWithTools.PromptTokens <= usageWithoutTools.PromptTokens {
			t.Errorf("Expected more PromptTokens with tools (%d) than without (%d)",
				usageWithTools.PromptTokens, usageWithoutTools.PromptTokens)
		}
	})
}

func TestShouldUsePolyfill(t *testing.T) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	t.Run("returns true for nil usage", func(t *testing.T) {
		if !tokenizer.ShouldUsePolyfill(nil) {
			t.Error("Expected ShouldUsePolyfill to return true for nil usage")
		}
	})

	t.Run("returns true for zero usage", func(t *testing.T) {
		usage := &sdk.CompletionUsage{
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		}
		if !tokenizer.ShouldUsePolyfill(usage) {
			t.Error("Expected ShouldUsePolyfill to return true for zero usage")
		}
	})

	t.Run("returns false for valid usage", func(t *testing.T) {
		usage := &sdk.CompletionUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		}
		if tokenizer.ShouldUsePolyfill(usage) {
			t.Error("Expected ShouldUsePolyfill to return false for valid usage")
		}
	})
}

func TestIsLikelyCodeContent(t *testing.T) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{
			name:     "plain English text",
			text:     "This is a simple sentence about the weather today.",
			expected: false,
		},
		{
			name:     "Go code",
			text:     `func main() { fmt.Println("Hello") }`,
			expected: true,
		},
		{
			name:     "Python code",
			text:     `def hello(): import os return "world"`,
			expected: true,
		},
		{
			name:     "JavaScript code",
			text:     `const foo = () => { let x = 1; return x; }`,
			expected: true,
		},
		{
			name:     "HTML content",
			text:     `<div><span>Hello</span><script>alert('hi');</script></div>`,
			expected: true,
		},
		{
			name:     "mixed content",
			text:     "The function does something with data.",
			expected: false, // Not enough code indicators
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tokenizer.IsLikelyCodeContent(tt.text)
			if result != tt.expected {
				t.Errorf("IsLikelyCodeContent(%q) = %v, expected %v",
					tt.text, result, tt.expected)
			}
		})
	}
}

func TestAdjustedEstimate(t *testing.T) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	t.Run("code content gets higher estimate", func(t *testing.T) {
		codeText := `func main() { fmt.Println("Hello, World!") }`
		proseText := "The quick brown fox jumps over the lazy dog."

		// Make the texts similar length
		codeEstimate := tokenizer.AdjustedEstimate(codeText)
		proseEstimate := tokenizer.AdjustedEstimate(proseText)

		// The base estimates should be similar since texts are similar length
		baseCodeEstimate := tokenizer.EstimateTokenCount(codeText)
		baseProseEstimate := tokenizer.EstimateTokenCount(proseText)

		// Code should be adjusted upward
		if codeEstimate <= baseCodeEstimate {
			t.Errorf("Expected adjusted code estimate (%d) to be higher than base (%d)",
				codeEstimate, baseCodeEstimate)
		}

		// Prose should not be adjusted
		if proseEstimate != baseProseEstimate {
			t.Errorf("Expected prose estimate (%d) to equal base (%d)",
				proseEstimate, baseProseEstimate)
		}
	})
}

func TestTokenizerConsistency(t *testing.T) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())

	// Test that the same input always produces the same output
	text := "This is a test message for consistency checking."

	firstEstimate := tokenizer.EstimateTokenCount(text)
	for i := 0; i < 100; i++ {
		estimate := tokenizer.EstimateTokenCount(text)
		if estimate != firstEstimate {
			t.Errorf("Inconsistent token count: expected %d, got %d on iteration %d",
				firstEstimate, estimate, i)
		}
	}
}

func BenchmarkEstimateTokenCount(b *testing.B) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())
	text := "The quick brown fox jumps over the lazy dog. This is a common pangram used for testing."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tokenizer.EstimateTokenCount(text)
	}
}

func BenchmarkCalculateUsagePolyfill(b *testing.B) {
	tokenizer := NewTokenizerService(DefaultTokenizerConfig())
	messages := []sdk.Message{
		{
			Role:    sdk.System,
			Content: sdk.NewMessageContent("You are a helpful assistant."),
		},
		{
			Role:    sdk.User,
			Content: sdk.NewMessageContent("What is the capital of France?"),
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tokenizer.CalculateUsagePolyfill(messages, "The capital of France is Paris.", nil, nil)
	}
}
