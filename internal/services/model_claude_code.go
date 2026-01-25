package services

import (
	"context"
	"fmt"

	"github.com/inference-gateway/cli/internal/domain"
)

// ClaudeCodeModelService provides a static list of Claude models available via subscription
// The Claude Code CLI doesn't provide model discovery, so we maintain a static list
type ClaudeCodeModelService struct {
	currentModel string
}

// NewClaudeCodeModelService creates a new Claude Code model service
func NewClaudeCodeModelService() domain.ModelService {
	return &ClaudeCodeModelService{
		currentModel: "claude-sonnet-4-5-20250929",
	}
}

// ListModels returns a static list of Claude models available via subscription
func (s *ClaudeCodeModelService) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"claude-opus-4-5",
		"claude-haiku-4-5-20251001",
		"claude-sonnet-4-5-20250929",
		"claude-opus-4-1-20250805",
		"claude-opus-4-20250514",
		"claude-sonnet-4-1-20250805",
		"claude-3-7-sonnet-20250219",
		"claude-3-5-haiku-20241022",
		"claude-3-haiku-20240307",
	}, nil
}

// SelectModel selects a model to use
func (s *ClaudeCodeModelService) SelectModel(modelID string) error {
	s.currentModel = modelID
	return nil
}

// GetCurrentModel returns the current model
func (s *ClaudeCodeModelService) GetCurrentModel() string {
	return s.currentModel
}

// IsModelAvailable checks if a model is available
func (s *ClaudeCodeModelService) IsModelAvailable(modelID string) bool {
	// Check if model is in our static list
	models, _ := s.ListModels(context.Background())
	for _, m := range models {
		if m == modelID {
			return true
		}
	}
	return false
}

// ValidateModel validates that a model exists
func (s *ClaudeCodeModelService) ValidateModel(modelID string) error {
	if !s.IsModelAvailable(modelID) {
		return fmt.Errorf("model '%s' is not available in Claude Code mode", modelID)
	}
	return nil
}

// IsVisionModel returns whether the model supports vision
// All modern Claude models support vision
func (s *ClaudeCodeModelService) IsVisionModel(model string) bool {
	return true
}
