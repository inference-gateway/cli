package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	tools "github.com/inference-gateway/cli/internal/services/tools"
	sdk "github.com/inference-gateway/sdk"
)

// LLMToolService implements ToolService with the new tools package architecture
type LLMToolService struct {
	registry *tools.Registry
	enabled  bool
	config   *config.Config
}

// NewLLMToolService creates a new LLM tool service with a new registry
func NewLLMToolService(cfg *config.Config) *LLMToolService {
	return &LLMToolService{
		registry: tools.NewRegistry(cfg),
		enabled:  cfg.Tools.Enabled,
		config:   cfg,
	}
}

// NewLLMToolServiceWithRegistry creates a new LLM tool service with an existing registry
func NewLLMToolServiceWithRegistry(cfg *config.Config, registry *tools.Registry) *LLMToolService {
	return &LLMToolService{
		registry: registry,
		enabled:  cfg.Tools.Enabled,
		config:   cfg,
	}
}

// ListTools returns definitions for all enabled tools
func (s *LLMToolService) ListTools() []sdk.ChatCompletionTool {
	if !s.enabled && !s.config.IsA2AToolsEnabled() {
		return []sdk.ChatCompletionTool{}
	}
	return s.registry.GetToolDefinitions()
}

// ListAvailableTools returns names of all enabled tools
func (s *LLMToolService) ListAvailableTools() []string {
	if !s.enabled && !s.config.IsA2AToolsEnabled() {
		return []string{}
	}
	return s.registry.ListAvailableTools()
}

// isA2ATool checks if a tool is an A2A-related tool
func (s *LLMToolService) isA2ATool(toolName string) bool {
	a2aTools := []string{"QueryAgent", "QueryTask", "Task"}
	for _, a2aTool := range a2aTools {
		if toolName == a2aTool {
			return true
		}
	}
	return false
}

// ExecuteTool executes a tool with the given arguments
func (s *LLMToolService) ExecuteTool(ctx context.Context, toolCall sdk.ChatCompletionMessageToolCallFunction) (*domain.ToolExecutionResult, error) {
	if !s.enabled {
		if !s.config.IsA2AToolsEnabled() || !s.isA2ATool(toolCall.Name) {
			return nil, fmt.Errorf("tools are not enabled")
		}
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Arguments), &args); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	tool, err := s.registry.GetTool(toolCall.Name)
	if err != nil {
		return nil, err
	}

	result, err := tool.Execute(ctx, args)

	if toolCall.Name == "Read" && err == nil && result != nil && result.Success {
		s.registry.SetReadToolUsed()
	}

	return result, err
}

// IsToolEnabled checks if a tool is enabled
func (s *LLMToolService) IsToolEnabled(name string) bool {
	if !s.enabled {
		if !s.config.IsA2AToolsEnabled() || !s.isA2ATool(name) {
			return false
		}
	}
	return s.registry.IsToolEnabled(name)
}

// ValidateTool validates tool arguments
func (s *LLMToolService) ValidateTool(name string, args map[string]any) error {
	if !s.enabled {
		if !s.config.IsA2AToolsEnabled() || !s.isA2ATool(name) {
			return fmt.Errorf("tools are not enabled")
		}
	}

	if strings.HasPrefix(name, "a2a_") {
		return nil
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

func (s *NoOpToolService) ListTools() []sdk.ChatCompletionTool {
	return []sdk.ChatCompletionTool{}
}

func (s *NoOpToolService) ListAvailableTools() []string {
	return []string{}
}

func (s *NoOpToolService) ExecuteTool(ctx context.Context, toolCall sdk.ChatCompletionMessageToolCallFunction) (*domain.ToolExecutionResult, error) {
	return nil, fmt.Errorf("tools are not enabled")
}

func (s *NoOpToolService) IsToolEnabled(name string) bool {
	return false
}

func (s *NoOpToolService) ValidateTool(name string, args map[string]any) error {
	return fmt.Errorf("tools are not enabled")
}
