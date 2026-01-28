// Package models provides utilities for working with LLM models.
package models

import (
	"strings"

	config "github.com/inference-gateway/cli/config"
)

// EstimateContextWindow returns an estimated context window size based on model name.
func EstimateContextWindow(model string) int {
	model = strings.ToLower(model)

	if idx := strings.Index(model, "/"); idx != -1 {
		model = model[idx+1:]
	}

	for _, matcher := range config.ContextMatchers {
		for _, pattern := range matcher.Patterns {
			if strings.Contains(model, pattern) {
				return matcher.ContextWindow
			}
		}
	}

	return config.DefaultContextWindow
}
