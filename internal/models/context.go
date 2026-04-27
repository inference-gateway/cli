// Package models provides utilities for working with LLM models.
package models

import (
	"strings"

	config "github.com/inference-gateway/cli/config"
)

// EstimateContextWindow returns an estimated context window size based on model name.
// Falls back to config.DefaultContextWindow when no matcher pattern hits.
func EstimateContextWindow(model string) int {
	window, _ := LookupContextWindow(model)
	return window
}

// LookupContextWindow returns the matched context window size and whether a
// matcher pattern actually hit. Callers that need to distinguish a real match
// from the default fallback (e.g. the model picker, which renders "?" for
// unknown models) should use this instead of EstimateContextWindow.
func LookupContextWindow(model string) (int, bool) {
	model = strings.ToLower(model)

	if idx := strings.Index(model, "/"); idx != -1 {
		model = model[idx+1:]
	}

	for _, matcher := range config.ContextMatchers {
		for _, pattern := range matcher.Patterns {
			if strings.Contains(model, pattern) {
				return matcher.ContextWindow, true
			}
		}
	}

	return config.DefaultContextWindow, false
}
