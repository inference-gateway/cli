package tools

import (
	"fmt"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
	sdk "github.com/inference-gateway/sdk"
)

// Registry manages all available tools
type Registry struct {
	config       *config.Config
	tools        map[string]domain.Tool
	readToolUsed bool
	taskTracker  domain.TaskTracker
}

// NewRegistry creates a new tool registry with self-contained tools
func NewRegistry(cfg *config.Config) *Registry {
	registry := &Registry{
		config:       cfg,
		tools:        make(map[string]domain.Tool),
		readToolUsed: false,
		taskTracker:  utils.NewTaskTracker(),
	}

	registry.registerTools()
	return registry
}

// registerTools initializes and registers all available tools
func (r *Registry) registerTools() {
	r.tools["Bash"] = NewBashTool(r.config)
	r.tools["Read"] = NewReadTool(r.config)
	r.tools["Write"] = NewWriteTool(r.config)
	r.tools["Edit"] = NewEditToolWithRegistry(r.config, r)
	r.tools["MultiEdit"] = NewMultiEditToolWithRegistry(r.config, r)
	r.tools["Delete"] = NewDeleteTool(r.config)
	r.tools["Grep"] = NewGrepTool(r.config)
	r.tools["Tree"] = NewTreeTool(r.config)
	r.tools["TodoWrite"] = NewTodoWriteTool(r.config)

	if r.config.Tools.WebFetch.Enabled {
		r.tools["WebFetch"] = NewWebFetchTool(r.config)
	}

	if r.config.Tools.WebSearch.Enabled {
		r.tools["WebSearch"] = NewWebSearchTool(r.config)
	}

	if r.config.Tools.Github.Enabled {
		r.tools["Github"] = NewGithubTool(r.config)
	}

	if r.config.IsA2AToolsEnabled() {
		r.tools["A2A_QueryAgent"] = NewA2AQueryAgentTool(r.config)
		r.tools["A2A_QueryTask"] = NewA2AQueryTaskTool(r.config, r.taskTracker)
		r.tools["A2A_SubmitTask"] = NewA2ASubmitTaskTool(r.config, r.taskTracker)
		r.tools["A2A_DownloadArtifacts"] = NewA2ADownloadArtifactsTool(r.config, r.taskTracker)
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
func (r *Registry) GetToolDefinitions() []sdk.ChatCompletionTool {
	var definitions []sdk.ChatCompletionTool
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

// SetReadToolUsed marks that the Read tool has been used
func (r *Registry) SetReadToolUsed() {
	r.readToolUsed = true
}

// IsReadToolUsed returns whether the Read tool has been used
func (r *Registry) IsReadToolUsed() bool {
	return r.readToolUsed
}

// GetTaskTracker returns the task tracker instance
func (r *Registry) GetTaskTracker() domain.TaskTracker {
	return r.taskTracker
}
