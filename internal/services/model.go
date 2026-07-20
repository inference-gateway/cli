package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	models "github.com/inference-gateway/cli/internal/models"
)

// visionModelPatterns contains known vision-capable model name patterns
// These patterns are matched as substrings against the model ID (case-insensitive)
var visionModelPatterns = []string{
	"gpt-4o",
	"gpt-4-turbo",
	"gpt-4-vision",
	"gpt-4v",
	"claude-3",
	"claude-3.5",
	"claude-4",
	"claude-sonnet",
	"claude-opus",
	"claude-haiku",
	"gemini-pro-vision",
	"gemini-1.5",
	"gemini-2",
	"gemini-flash",
	"gemini-ultra",
	"llava",
	"bakllava",
	"moondream",
	"cogvlm",
	"qwen-vl",
	"internvl",
	"minicpm-v",
	"phi-3-vision",
	"llama-3.2-vision",
}

// HTTPModelService implements ModelService using SDK client
type HTTPModelService struct {
	client    sdk.Client
	current   string
	models    []string
	modelsMux sync.RWMutex
	lastFetch time.Time
	cacheTTL  time.Duration
}

// NewHTTPModelService creates a new HTTP-based model service with pre-configured client
func NewHTTPModelService(client sdk.Client) *HTTPModelService {
	return &HTTPModelService{
		client:   client,
		cacheTTL: 5 * time.Minute,
	}
}

func (s *HTTPModelService) ListModels(ctx context.Context) ([]string, error) {
	s.modelsMux.RLock()
	if time.Since(s.lastFetch) < s.cacheTTL && len(s.models) > 0 {
		result := make([]string, len(s.models))
		copy(result, s.models)
		s.modelsMux.RUnlock()
		return result, nil
	}
	s.modelsMux.RUnlock()

	if s.client == nil {
		return nil, fmt.Errorf("SDK client is not initialized")
	}

	resp, err := s.client.ListModels(ctx, sdk.ListModelsParamsIncludeContextWindow, sdk.ListModelsParamsIncludePricing)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	if resp == nil || resp.Data == nil {
		return nil, fmt.Errorf("empty response from models API")
	}

	ids := make([]string, len(resp.Data))
	windows := make(map[string]int, len(resp.Data))
	prices := make(map[string]gatewayPrice, len(resp.Data))
	for i, model := range resp.Data {
		ids[i] = model.ID
		if cw := model.ContextWindow; cw != nil && cw.Tokens > 0 {
			windows[model.ID] = cw.Tokens
		}
		if price, ok := parseGatewayPricing(model.Pricing); ok {
			prices[model.ID] = price
		}
	}

	s.modelsMux.Lock()
	s.models = ids
	s.lastFetch = time.Now()
	s.modelsMux.Unlock()

	if len(windows) > 0 {
		models.SetGatewayContextWindows(windows)
	}
	if len(prices) > 0 {
		setGatewayPricing(prices)
	}

	result := make([]string, len(ids))
	copy(result, ids)
	return result, nil
}

func (s *HTTPModelService) SelectModel(modelID string) error {
	if err := s.ValidateModel(modelID); err != nil {
		return fmt.Errorf("invalid model: %w", err)
	}

	s.current = modelID
	return nil
}

func (s *HTTPModelService) GetCurrentModel() string {
	return s.current
}

func (s *HTTPModelService) IsModelAvailable(modelID string) bool {
	s.modelsMux.RLock()
	defer s.modelsMux.RUnlock()

	for _, model := range s.models {
		if model == modelID {
			return true
		}
	}
	return false
}

func (s *HTTPModelService) ValidateModel(modelID string) error {
	if modelID == "" {
		return fmt.Errorf("model ID cannot be empty")
	}

	s.modelsMux.RLock()
	cachedModels := s.models
	s.modelsMux.RUnlock()

	if len(cachedModels) > 0 {
		return s.validateAgainstCachedModels(modelID, cachedModels)
	}

	return s.validateAgainstFetchedModels(modelID)
}

func (s *HTTPModelService) validateAgainstCachedModels(modelID string, models []string) error {
	for _, model := range models {
		if model == modelID {
			return nil
		}
	}
	return fmt.Errorf("model '%s' is not available", modelID)
}

func (s *HTTPModelService) validateAgainstFetchedModels(modelID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	models, err := s.ListModels(ctx)
	if err != nil {
		return s.handleListModelsError(modelID, err)
	}

	for _, model := range models {
		if model == modelID {
			return nil
		}
	}

	return fmt.Errorf("model '%s' is not available", modelID)
}

func (s *HTTPModelService) handleListModelsError(modelID string, _ /* err */ error) error {
	if !isValidModelFormat(modelID) {
		return fmt.Errorf("invalid model ID format: %s", modelID)
	}
	return nil
}

// isValidModelFormat performs basic format validation on model IDs
func isValidModelFormat(modelID string) bool {
	return strings.Contains(modelID, "/") && len(modelID) > 3
}

// IsVisionModel checks if a model supports vision/image input capabilities
// It uses a hardcoded list of known vision-capable model patterns
func (s *HTTPModelService) IsVisionModel(modelID string) bool {
	lowerModelID := strings.ToLower(modelID)
	for _, pattern := range visionModelPatterns {
		if strings.Contains(lowerModelID, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}
