package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

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

	resp, err := s.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	if resp == nil || resp.Data == nil {
		return nil, fmt.Errorf("empty response from models API")
	}

	models := make([]string, len(resp.Data))
	for i, model := range resp.Data {
		models[i] = model.Id
	}

	s.modelsMux.Lock()
	s.models = models
	s.lastFetch = time.Now()
	s.modelsMux.Unlock()

	result := make([]string, len(models))
	copy(result, models)
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
