package tools

import (
	"fmt"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// Registry manages all available tools
type Registry struct {
	config           *config.Config
	fileService      domain.FileService
	fetchService     domain.FetchService
	webSearchService domain.WebSearchService
	tools            map[string]Tool
}

// NewRegistry creates a new tool registry
func NewRegistry(cfg *config.Config, fileService domain.FileService, fetchService domain.FetchService, webSearchService domain.WebSearchService) *Registry {
	registry := &Registry{
		config:           cfg,
		fileService:      fileService,
		fetchService:     fetchService,
		webSearchService: webSearchService,
		tools:            make(map[string]Tool),
	}

	registry.registerTools()
	return registry
}

// registerTools initializes and registers all available tools
func (r *Registry) registerTools() {
	// Core tools (always available when tools are enabled)
	r.tools["Bash"] = NewBashTool(r.config)
	r.tools["Read"] = NewReadTool(r.config, r.fileService)
	r.tools["FileSearch"] = NewFileSearchTool(r.config)

	// Conditional tools
	if r.config.Fetch.Enabled {
		r.tools["Fetch"] = NewFetchTool(r.config, r.fetchService)
	}

	if r.config.WebSearch.Enabled {
		r.tools["WebSearch"] = NewWebSearchTool(r.config, r.webSearchService)
	}
}

// GetTool retrieves a tool by name
func (r *Registry) GetTool(name string) (Tool, error) {
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