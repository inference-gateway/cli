// Package models provides utilities for working with LLM models.
package models

import (
	"slices"
	"strings"
	"sync"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

var (
	gatewayMu         sync.RWMutex
	gatewayWindows    map[string]int
	gatewayModalities map[string]sdk.ModelModalities
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
func SetGatewayModalities(modalities map[string]sdk.ModelModalities) {
	normalized := make(map[string]sdk.ModelModalities, len(modalities))
	for id, mods := range modalities {
		normalized[strings.ToLower(id)] = mods
	}
	gatewayMu.Lock()
	gatewayModalities = normalized
	gatewayMu.Unlock()
}

// IsImageGenModalities reports whether a modality set describes an
// image-generation model: it outputs "image" but not "text", so it can
// produce pictures but cannot chat. Vision chat models (image in, text out)
// return false.
func IsImageGenModalities(mods sdk.ModelModalities) bool {
	return slices.Contains(mods.Output, sdk.ModalityImage) &&
		!slices.Contains(mods.Output, sdk.ModalityText)
}

// IsChatCapableModalities reports whether a modality set describes a model
// that can serve /chat/completions: it accepts "text" input and produces
// "text" output. Speech-to-text (audio in), text-to-speech (audio out),
// image generation and video models all fail this predicate. Modality sets
// that do not include text at all are naturally rejected.
func IsChatCapableModalities(mods sdk.ModelModalities) bool {
	return slices.Contains(mods.Input, sdk.ModalityText) &&
		slices.Contains(mods.Output, sdk.ModalityText)
}

// HasReportedModalities reports whether a modality set carries any data at
// all. An empty set means the gateway did not report modalities for the
// model, so callers must treat it as unknown and keep it listed instead of
// filtering on a capability nobody reported.
func HasReportedModalities(mods sdk.ModelModalities) bool {
	return len(mods.Input) > 0 || len(mods.Output) > 0
}

// modelModalities returns the registry entry for a model; the zero value
// (empty input/output) stands in for unknown models, making every predicate
// below false.
func modelModalities(modelID string) sdk.ModelModalities {
	gatewayMu.RLock()
	defer gatewayMu.RUnlock()
	return gatewayModalities[strings.ToLower(modelID)]
}

// SupportsVision reports whether the model accepts native image input
// (input modalities include both "text" and "image"). Models not in the
// registry return false.
func SupportsVision(modelID string) bool {
	mods := modelModalities(modelID)
	return slices.Contains(mods.Input, sdk.ModalityText) &&
		slices.Contains(mods.Input, sdk.ModalityImage)
}

// IsImageGenerationModel reports whether the model generates images rather
// than text (see IsImageGenModalities). Falls back to false when the model
// is not in the registry.
func IsImageGenerationModel(modelID string) bool {
	return IsImageGenModalities(modelModalities(modelID))
}

// ModalitiesLabel returns a compact human-readable label for a model's
// modalities, or "" when the model is not in the registry: "image-gen" for
// image-generation models, "vision" for image-accepting chat models, ""
// otherwise.
func ModalitiesLabel(modelID string) string {
	switch {
	case IsImageGenerationModel(modelID):
		return "image-gen"
	case SupportsVision(modelID):
		return "vision"
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
