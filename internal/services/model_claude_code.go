package services

import (
	"context"
	"fmt"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// ClaudeCodeModelService provides a static list of Claude models available via subscription
// The Claude Code CLI doesn't provide model discovery, so we maintain a static list
type ClaudeCodeModelService struct {
	currentModel string
}

// NewClaudeCodeModelService creates a new Claude Code model service
func NewClaudeCodeModelService() domain.ModelService {
	return &ClaudeCodeModelService{
		currentModel: "anthropic/claude-sonnet-4-5-20250929",
	}
}

// ListModels returns a static list of Claude models available via subscription.
// The ids are anthropic/-prefixed to stay consistent with gateway mode and the
// pricing table (config.DefaultModelPricing); the set is curated to ids that the
// pricing table prices, so session cost can be derived from token counts. The
// adapter strips the prefix before invoking the claude CLI.
func (s *ClaudeCodeModelService) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"anthropic/claude-opus-4-7",
		"anthropic/claude-opus-4-6",
		"anthropic/claude-opus-4-5-20251101",
		"anthropic/claude-opus-4-1-20250805",
		"anthropic/claude-opus-4-20250514",
		"anthropic/claude-sonnet-4-6",
		"anthropic/claude-sonnet-4-5-20250929",
		"anthropic/claude-sonnet-4-20250514",
		"anthropic/claude-haiku-4-5-20251001",
	}, nil
}

// CanonicalClaudeModelID normalizes a bare Claude id to its anthropic/-prefixed
// form so callers may pass either "claude-..." (back-compat) or
// "anthropic/claude-..." (canonical). Already-prefixed or non-Claude ids pass
// through unchanged.
func CanonicalClaudeModelID(modelID string) string {
	if strings.HasPrefix(modelID, "claude-") {
		return "anthropic/" + modelID
	}
	return modelID
}

// SelectModel selects a model to use
func (s *ClaudeCodeModelService) SelectModel(modelID string) error {
	s.currentModel = CanonicalClaudeModelID(modelID)
	return nil
}

// GetCurrentModel returns the current model
func (s *ClaudeCodeModelService) GetCurrentModel() string {
	return s.currentModel
}

// IsModelAvailable checks if a model is available
func (s *ClaudeCodeModelService) IsModelAvailable(modelID string) bool {
	target := CanonicalClaudeModelID(modelID)
	models, _ := s.ListModels(context.Background())
	for _, m := range models {
		if m == target {
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
