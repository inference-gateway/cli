package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPModelService implements ModelService using HTTP API calls
type HTTPModelService struct {
	baseURL   string
	apiKey    string
	client    *http.Client
	current   string
	models    []string
	modelsMux sync.RWMutex
	lastFetch time.Time
	cacheTTL  time.Duration
}

// ModelsResponse represents the API response for listing models
type ModelsResponse struct {
	Data []struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	} `json:"data"`
}

// NewHTTPModelService creates a new HTTP-based model service
func NewHTTPModelService(baseURL, apiKey string) *HTTPModelService {
	return &HTTPModelService{
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 30 * time.Second},
		cacheTTL: 5 * time.Minute, // Cache models for 5 minutes
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

	url := fmt.Sprintf("%s/v1/models", s.baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	var modelsResp ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	models := make([]string, len(modelsResp.Data))
	for i, model := range modelsResp.Data {
		models[i] = model.ID
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
	if len(s.models) > 0 {
		available := false
		for _, model := range s.models {
			if model == modelID {
				available = true
				break
			}
		}
		s.modelsMux.RUnlock()

		if !available {
			return fmt.Errorf("model '%s' is not available", modelID)
		}
	} else {
		s.modelsMux.RUnlock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		models, err := s.ListModels(ctx)
		if err != nil {
			if !isValidModelFormat(modelID) {
				return fmt.Errorf("invalid model ID format: %s", modelID)
			}
			return nil
		}

		available := false
		for _, model := range models {
			if model == modelID {
				available = true
				break
			}
		}

		if !available {
			return fmt.Errorf("model '%s' is not available", modelID)
		}
	}

	return nil
}

// isValidModelFormat performs basic format validation on model IDs
func isValidModelFormat(modelID string) bool {
	return strings.Contains(modelID, "/") && len(modelID) > 3
}
