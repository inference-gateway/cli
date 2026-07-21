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

// LookupContextWindow returns the matched context window size and whether a
// real match was found (from user config override or gateway data). Unknown
// models return (0, false) - there is no built-in fallback, so callers must
// gate window-dependent features on the second return.
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

	return 0, false
}
