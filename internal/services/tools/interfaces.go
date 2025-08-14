package tools

import (
	"context"

	"github.com/inference-gateway/cli/internal/domain"
)

// Tool represents a single tool with its definition, handler, and validator
type Tool interface {
	// Definition returns the tool definition for the LLM
	Definition() domain.ToolDefinition
	
	// Execute runs the tool with given arguments
	Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error)
	
	// Validate checks if the tool arguments are valid
	Validate(args map[string]interface{}) error
	
	// IsEnabled returns whether this tool is enabled
	IsEnabled() bool
}

// ToolFactory creates tool instances
type ToolFactory interface {
	// CreateTool creates a tool instance by name
	CreateTool(name string) (Tool, error)
	
	// ListAvailableTools returns names of all available tools
	ListAvailableTools() []string
}