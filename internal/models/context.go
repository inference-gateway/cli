// Package models provides utilities for working with LLM models.
package models

import "strings"

// modelMatcher defines a pattern match for context window estimation.
type modelMatcher struct {
	patterns      []string
	contextWindow int
}

// contextMatchers defines all model patterns in priority order.
// More specific patterns must come before less specific ones.
var contextMatchers = []modelMatcher{
	{patterns: []string{"deepseek"}, contextWindow: 128000},
	{patterns: []string{"o1", "o3"}, contextWindow: 200000},
	{patterns: []string{"gpt-4o", "gpt-4-turbo"}, contextWindow: 128000},
	{patterns: []string{"gpt-4-32k"}, contextWindow: 32768},
	{patterns: []string{"gpt-4"}, contextWindow: 8192},
	{patterns: []string{"gpt-3.5"}, contextWindow: 16384},
	{patterns: []string{"claude-4", "claude-3.5", "claude-3"}, contextWindow: 200000},
	{patterns: []string{"claude-2"}, contextWindow: 100000},
	{patterns: []string{"claude"}, contextWindow: 200000},
	{patterns: []string{"gemini-2", "gemini-1.5"}, contextWindow: 1000000},
	{patterns: []string{"gemini"}, contextWindow: 32768},
	{patterns: []string{"mistral-large"}, contextWindow: 128000},
	{patterns: []string{"mistral", "mixtral"}, contextWindow: 32768},
	{patterns: []string{"llama-3.1", "llama-3.2", "llama-3.3"}, contextWindow: 128000},
	{patterns: []string{"llama-3"}, contextWindow: 8192},
	{patterns: []string{"llama"}, contextWindow: 4096},
	{patterns: []string{"qwen3", "qwen-3"}, contextWindow: 262144},
	{patterns: []string{"qwen2.5", "qwen-2.5"}, contextWindow: 131072},
	{patterns: []string{"qwen"}, contextWindow: 128000},
	{patterns: []string{"command-r"}, contextWindow: 128000},
}

const defaultContextWindow = 8192

// EstimateContextWindow returns an estimated context window size based on model name.
func EstimateContextWindow(model string) int {
	model = strings.ToLower(model)

	if idx := strings.Index(model, "/"); idx != -1 {
		model = model[idx+1:]
	}

	for _, matcher := range contextMatchers {
		for _, pattern := range matcher.patterns {
			if strings.Contains(model, pattern) {
				return matcher.contextWindow
			}
		}
	}

	return defaultContextWindow
}
