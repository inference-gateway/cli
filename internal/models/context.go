// Package models provides utilities for working with LLM models.
package models

import (
	"strings"
	"sync"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

var (
	gatewayMu         sync.RWMutex
	gatewayWindows    map[string]int
	gatewayModalities map[string][]sdk.ModelModalities
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

// SetGatewayModalities replaces the gateway-reported modalities (from
// /v1/models?include=modalities). Keys are full "provider/model" ids;
// matching is exact on the lowercased id.
func SetGatewayModalities(modalities map[string][]sdk.ModelModalities) {
	normalized := make(map[string][]sdk.ModelModalities, len(modalities))
	for id, mods := range modalities {
		normalized[strings.ToLower(id)] = mods
	}
	gatewayMu.Lock()
	gatewayModalities = normalized
	gatewayMu.Unlock()
}

// TextImage reports whether a modality list contains "text" and "image"
// respectively. All modality-derived predicates route through this one loop.
func TextImage(mods []sdk.ModelModalities) (hasText, hasImage bool) {
	for _, m := range mods {
		switch m {
		case sdk.ModelModalitiesText:
			hasText = true
		case sdk.ModelModalitiesImage:
			hasImage = true
		}
	}
	return hasText, hasImage
}

func modelModalities(modelID string) []sdk.ModelModalities {
	gatewayMu.RLock()
	defer gatewayMu.RUnlock()
	return gatewayModalities[strings.ToLower(modelID)]
}

// SupportsVision reports whether the model supports native image input
// (modalities include both "text" and "image"). Image-generation models
// (dall-e, flux) have "image" only and return false. Models not in the
// registry or with nil modalities also return false.
func SupportsVision(modelID string) bool {
	hasText, hasImage := TextImage(modelModalities(modelID))
	return hasText && hasImage
}

// IsImageGenerationModel reports whether the model generates images rather
// than text (modalities include "image" but NOT "text"). Falls back to
// false when the model is not in the registry.
func IsImageGenerationModel(modelID string) bool {
	hasText, hasImage := TextImage(modelModalities(modelID))
	return hasImage && !hasText
}

// ModalitiesLabel returns a compact human-readable label for a model's
// modalities, or "" when the model is not in the registry. Vision models
// (text+image) get "vision"; image-generation models (image only) get
// "image-gen"; text-only models get "".
func ModalitiesLabel(modelID string) string {
	hasText, hasImage := TextImage(modelModalities(modelID))
	switch {
	case hasText && hasImage:
		return "vision"
	case hasImage && !hasText:
		return "image-gen"
	default:
		return ""
	}
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
