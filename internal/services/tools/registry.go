package tools

import (
	"fmt"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// Registry manages all available tools
type Registry struct {
	config *config.Config
	tools  map[string]domain.Tool
}

// NewRegistry creates a new tool registry with self-contained tools
func NewRegistry(cfg *config.Config) *Registry {
	registry := &Registry{
		config: cfg,
		tools:  make(map[string]domain.Tool),
	}

	registry.registerTools()
	return registry
}

// registerTools initializes and registers all available tools
func (r *Registry) registerTools() {
	r.tools["Bash"] = NewBashTool(r.config)
	r.tools["Read"] = NewReadTool(r.config)
	r.tools["FileSearch"] = NewFileSearchTool(r.config)
	r.tools["Tree"] = NewTreeTool(r.config)
	r.tools["WriteTodos"] = NewWriteTodosTool(r.config)

	if r.config.Tools.Fetch.Enabled {
		r.tools["Fetch"] = NewFetchTool(r.config)
	}

	if r.config.Tools.WebSearch.Enabled {
		r.tools["WebSearch"] = NewWebSearchTool(r.config)
	}
}

// GetTool retrieves a tool by name
func (r *Registry) GetTool(name string) (domain.Tool, error) {
	tool, exists := r.tools[name]
	if !exists {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	return tool, nil
}

// ListAvailableTools returns names of all available and enabled tools
func (r *Registry) ListAvailableTools() []string {
	var tools []string
	for name, tool := range r.tools {
		if tool.IsEnabled() {
			tools = append(tools, name)
		}
	}
	return tools
}

// GetToolDefinitions returns definitions for all enabled tools
func (r *Registry) GetToolDefinitions() []domain.ToolDefinition {
	var definitions []domain.ToolDefinition
	for _, tool := range r.tools {
		if tool.IsEnabled() {
			definitions = append(definitions, tool.Definition())
		}
	}
	return definitions
}

// IsToolEnabled checks if a specific tool is enabled
func (r *Registry) IsToolEnabled(name string) bool {
	tool, exists := r.tools[name]
	if !exists {
		return false
	}
	return tool.IsEnabled()
}
