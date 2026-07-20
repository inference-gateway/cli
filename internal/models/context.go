// Package models provides utilities for working with LLM models.
package models

import (
	"strings"
	"sync"

	config "github.com/inference-gateway/cli/config"
)

var (
	gatewayMu      sync.RWMutex
	gatewayWindows map[string]int
)

// SetGatewayContextWindows replaces the gateway-reported context windows
// (from /v1/models?include=context_window). Keys are full "provider/model"
// ids; matching is exact on the lowercased id.
func SetGatewayContextWindows(windows map[string]int) {
	normalized := make(map[string]int, len(windows))
	for id, tokens := range windows {
		normalized[strings.ToLower(id)] = tokens
	}
	gatewayMu.Lock()
	gatewayWindows = normalized
	gatewayMu.Unlock()
}

func gatewayContextWindow(fullID string) (int, bool) {
	gatewayMu.RLock()
	defer gatewayMu.RUnlock()
	window, ok := gatewayWindows[fullID]
	return window, ok
}

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
	fullID := model

	if idx := strings.Index(model, "/"); idx != -1 {
		model = model[idx+1:]
	}

	bestLen := -1
	bestWindow := 0
	for pattern, window := range config.UserContextWindows {
		p := strings.ToLower(pattern)
		if strings.Contains(model, p) && len(p) > bestLen {
			bestLen = len(p)
			bestWindow = window
		}
	}
	if bestLen >= 0 {
		return bestWindow, true
	}

	if window, ok := gatewayContextWindow(fullID); ok && window > 0 {
		return window, true
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
