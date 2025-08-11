package internal

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/config"
)

// LLMToolsManager provides tools that LLMs can invoke
type LLMToolsManager struct {
	toolEngine *ToolEngine
}

// NewLLMToolsManager creates a new LLM tools manager
func NewLLMToolsManager(cfg *config.Config) *LLMToolsManager {
	return &LLMToolsManager{
		toolEngine: NewToolEngine(cfg),
	}
}

// NewLLMToolsManagerWithUI creates a new LLM tools manager with UI integration
func NewLLMToolsManagerWithUI(cfg *config.Config, program *tea.Program, inputModel *ChatInputModel) *LLMToolsManager {
	return &LLMToolsManager{
		toolEngine: NewToolEngineWithUI(cfg, program, inputModel),
	}
}

// BashTool represents the Bash tool that LLMs can invoke
type BashTool struct {
	manager *LLMToolsManager
}

// NewBashTool creates a new Bash tool
func NewBashTool(manager *LLMToolsManager) *BashTool {
	return &BashTool{manager: manager}
}

// Execute executes a bash command and returns the result
func (bt *BashTool) Execute(command string) (string, error) {
	result, err := bt.manager.toolEngine.ExecuteBash(command)
	if err != nil {
		return "", err
	}

	output := fmt.Sprintf("Command: %s\n", result.Command)
	output += fmt.Sprintf("Exit Code: %d\n", result.ExitCode)
	output += fmt.Sprintf("Duration: %s\n", result.Duration)

	if result.Error != "" {
		output += fmt.Sprintf("Error: %s\n", result.Error)
	}

	output += fmt.Sprintf("Output:\n%s", result.Output)

	return output, nil
}

// ExecuteJSON executes a bash command and returns JSON result
func (bt *BashTool) ExecuteJSON(command string) (string, error) {
	result, err := bt.manager.toolEngine.ExecuteBash(command)
	if err != nil {
		return "", err
	}

	jsonOutput, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return string(jsonOutput), nil
}

// ToolDefinition represents a tool definition for LLMs
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// GetAvailableTools returns the list of available tools for LLMs
func (manager *LLMToolsManager) GetAvailableTools() []ToolDefinition {
	if !manager.toolEngine.config.Tools.Enabled {
		return []ToolDefinition{}
	}

	return []ToolDefinition{
		{
			Name:        "Bash",
			Description: "Execute whitelisted bash commands securely",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The bash command to execute",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (text or json)",
						"enum":        []string{"text", "json"},
						"default":     "text",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

// InvokeTool invokes a tool by name with given parameters
func (manager *LLMToolsManager) InvokeTool(toolName string, parameters map[string]interface{}) (string, error) {
	switch toolName {
	case "Bash":
		command, ok := parameters["command"].(string)
		if !ok {
			return "", fmt.Errorf("command parameter is required and must be a string")
		}

		format, ok := parameters["format"].(string)
		if !ok {
			format = "text"
		}

		bashTool := NewBashTool(manager)
		if format == "json" {
			return bashTool.ExecuteJSON(command)
		}
		return bashTool.Execute(command)

	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}
