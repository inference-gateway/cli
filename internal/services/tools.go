package services

import (
	"context"
	"fmt"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services/tools"
)

// LLMToolService implements ToolService with the new tools package architecture
type LLMToolService struct {
	registry *tools.Registry
	enabled  bool
}

// NewLLMToolService creates a new LLM tool service with a new registry
func NewLLMToolService(cfg *config.Config) *LLMToolService {
	return &LLMToolService{
		registry: tools.NewRegistry(cfg),
		enabled:  cfg.Tools.Enabled,
	}
}

// NewLLMToolServiceWithRegistry creates a new LLM tool service with an existing registry
func NewLLMToolServiceWithRegistry(cfg *config.Config, registry *tools.Registry) *LLMToolService {
	return &LLMToolService{
		registry: registry,
		enabled:  cfg.Tools.Enabled,
	}
}

// ListTools returns definitions for all enabled tools
func (s *LLMToolService) ListTools() []domain.ToolDefinition {
	if !s.enabled {
		return []domain.ToolDefinition{}
	}
	return s.registry.GetToolDefinitions()
}

// ExecuteTool executes a tool with the given arguments
func (s *LLMToolService) ExecuteTool(ctx context.Context, name string, args map[string]any) (*domain.ToolExecutionResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("tools are not enabled")
	}

	tool, err := s.registry.GetTool(name)
	if err != nil {
		return nil, err
	}

	result, err := tool.Execute(ctx, args)

	if name == "Read" && err == nil && result != nil && result.Success {
		s.registry.SetReadToolUsed()
	}

	return result, err
}

// IsToolEnabled checks if a tool is enabled
func (s *LLMToolService) IsToolEnabled(name string) bool {
	if !s.enabled {
		return false
	}
	return s.registry.IsToolEnabled(name)
}

// ValidateTool validates tool arguments
func (s *LLMToolService) ValidateTool(name string, args map[string]any) error {
	if !s.enabled {
		return fmt.Errorf("tools are not enabled")
	}

	tool, err := s.registry.GetTool(name)
	if err != nil {
		return fmt.Errorf("tool '%s' is not available", name)
	}

	return tool.Validate(args)
}

// NoOpToolService implements ToolService as a no-op (when tools are disabled)
type NoOpToolService struct{}

// NewNoOpToolService creates a new no-op tool service
func NewNoOpToolService() *NoOpToolService {
	return &NoOpToolService{}
}

func (s *NoOpToolService) ListTools() []domain.ToolDefinition {
	return []domain.ToolDefinition{}
}

func (s *NoOpToolService) ExecuteTool(ctx context.Context, name string, args map[string]any) (*domain.ToolExecutionResult, error) {
	return nil, fmt.Errorf("tools are not enabled")
}

func (s *NoOpToolService) IsToolEnabled(name string) bool {
	return false
}

func (s *NoOpToolService) ValidateTool(name string, args map[string]any) error {
	return fmt.Errorf("tools are not enabled")
}
