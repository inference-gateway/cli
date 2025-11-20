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

// isToolEnabled checks if a tool should be included based on its type and configuration
func (s *LLMToolService) isToolEnabled(toolName string) bool {
	if s.isA2ATool(toolName) {
		return s.config.IsA2AToolsEnabled() && s.registry.IsToolEnabled(toolName)
	}
	return s.enabled && s.registry.IsToolEnabled(toolName)
}

// ListTools returns definitions for all enabled tools
func (s *LLMToolService) ListTools() []sdk.ChatCompletionTool {
	var definitions []sdk.ChatCompletionTool

	allTools := s.registry.GetToolDefinitions()
	for _, tool := range allTools {
		if s.isToolEnabled(tool.Function.Name) {
			definitions = append(definitions, tool)
		}
	}

	return definitions
}

// ListToolsForMode returns definitions for enabled tools filtered by agent mode
func (s *LLMToolService) ListToolsForMode(mode domain.AgentMode) []sdk.ChatCompletionTool {
	if mode == domain.AgentModePlan {
		allowedTools := map[string]bool{
			"Read":           true,
			"Grep":           true,
			"Tree":           true,
			"A2A_QueryAgent": true,
		}

		var definitions []sdk.ChatCompletionTool
		allTools := s.registry.GetToolDefinitions()
		for _, tool := range allTools {
			if s.isToolEnabled(tool.Function.Name) && allowedTools[tool.Function.Name] {
				definitions = append(definitions, tool)
			}
		}
		return definitions
	}

	allTools := s.ListTools()
	return allTools
}

// ListAvailableTools returns names of all enabled tools
func (s *LLMToolService) ListAvailableTools() []string {
	var tools []string

	allTools := s.registry.ListAvailableTools()
	for _, toolName := range allTools {
		if s.isToolEnabled(toolName) {
			tools = append(tools, toolName)
		}
	}

	return tools
}

// isA2ATool checks if a tool is an A2A-related tool
func (s *LLMToolService) isA2ATool(toolName string) bool {
	return strings.HasPrefix(toolName, "A2A_")
}

// ExecuteTool executes a tool with the given arguments
func (s *LLMToolService) ExecuteTool(ctx context.Context, toolCall sdk.ChatCompletionMessageToolCallFunction) (*domain.ToolExecutionResult, error) {
	if !s.isToolEnabled(toolCall.Name) {
		if s.isA2ATool(toolCall.Name) {
			return nil, fmt.Errorf("A2A tools are not enabled")
		}
		return nil, fmt.Errorf("local tools are not enabled")
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
	return s.isToolEnabled(name)
}

// ValidateTool validates tool arguments
func (s *LLMToolService) ValidateTool(name string, args map[string]any) error {
	if !s.isToolEnabled(name) {
		if s.isA2ATool(name) {
			return fmt.Errorf("A2A tools are not enabled")
		}
		return fmt.Errorf("local tools are not enabled")
	}

	if s.isA2ATool(name) {
		return nil
	}

	tool, err := s.registry.GetTool(name)
	if err != nil {
		return fmt.Errorf("tool '%s' is not available", name)
	}

	return tool.Validate(args)
}

func (s *LLMToolService) GetTaskTracker() domain.TaskTracker {
	return s.registry.GetTaskTracker()
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

func (s *NoOpToolService) ListToolsForMode(mode domain.AgentMode) []sdk.ChatCompletionTool {
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

func (s *NoOpToolService) GetTaskTracker() domain.TaskTracker {
	return nil
}
