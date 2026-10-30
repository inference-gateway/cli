package conversation

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	models "github.com/inference-gateway/cli/internal/platform/models"
)

// HTTPModelService implements ModelService using SDK client
type HTTPModelService struct {
	client    sdk.Client
	current   string
	models    []string
	catalog   []sdk.Model
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

	resp, err := s.client.ListModels(ctx, sdk.ListModelsParamsIncludeContextWindow, sdk.ListModelsParamsIncludePricing, sdk.ListModelsParamsIncludeModalities)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	if resp == nil || resp.Data == nil {
		return nil, fmt.Errorf("empty response from models API")
	}

	ids := RecordModels(resp.Data)

	s.modelsMux.Lock()
	s.models = ids
	s.catalog = resp.Data
	s.lastFetch = time.Now()
	s.modelsMux.Unlock()

	result := make([]string, len(ids))
	copy(result, ids)
	return result, nil
}

// Catalog is the gateway's models as last fetched, with their context
// windows, prices and modalities, for a client that reads them over a daemon.
func (s *HTTPModelService) Catalog() []sdk.Model {
	s.modelsMux.RLock()
	defer s.modelsMux.RUnlock()
	return slices.Clone(s.catalog)
}

// RecordModels publishes the context windows, prices and modalities of the
// gateway's models to this process and returns the ids of the chat models.
// Models whose modalities the gateway does not report are left out.
func RecordModels(catalog []sdk.Model) []string {
	ids := make([]string, 0, len(catalog))
	nonChat := make([]string, 0, len(catalog))
	windows := make(map[string]int, len(catalog))
	prices := make(map[string]gatewayPrice, len(catalog))
	modalities := make(map[string]sdk.ModelModalities, len(catalog))
	for _, model := range catalog {
		if model.Modalities == nil {
			continue
		}
		modalities[model.ID] = *model.Modalities
		if !models.IsChatCapableModalities(*model.Modalities) {
			nonChat = append(nonChat, model.ID)
			continue
		}
		ids = append(ids, model.ID)
		if cw := model.ContextWindow; cw != nil && cw.Tokens > 0 {
			windows[model.ID] = cw.Tokens
		}
		if price, ok := parseGatewayPricing(model.Pricing); ok {
			prices[model.ID] = price
		}
	}

	if len(windows) > 0 {
		models.SetGatewayContextWindows(windows)
	}
	if len(prices) > 0 {
		setGatewayPricing(prices)
	}
	if len(modalities) > 0 {
		models.SetGatewayModalities(modalities)
	}
	models.SetNonChatModels(nonChat)
	return ids
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
