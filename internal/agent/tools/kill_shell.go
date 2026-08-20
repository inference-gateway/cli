package tools

import (
	"context"
	"fmt"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"

	sdk "github.com/inference-gateway/sdk"

	"github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// KillShellTool cancels a running background shell
type KillShellTool struct {
	config       *config.Config
	shellService scheddomain.BackgroundShellService
	enabled      bool
}

// NewKillShellTool creates a new KillShell tool
func NewKillShellTool(cfg *config.Config, shellService scheddomain.BackgroundShellService) *KillShellTool {
	return &KillShellTool{
		config:       cfg,
		shellService: shellService,
		enabled:      cfg.Tools.Enabled && cfg.Tools.Bash.BackgroundShells.Enabled,
	}
}

// Definition returns the tool definition for the SDK
func (t *KillShellTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.KillShell.Description

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "KillShell",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"shell_id": map[string]any{
						"type":        "string",
						"description": "The ID of the background shell to kill",
					},
				},
				"required":             []string{"shell_id"},
				"additionalProperties": false,
			},
		},
	}
}

// Execute cancels a background shell
func (t *KillShellTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	shellID, _ := args["shell_id"].(string)

	shell := t.shellService.GetShell(shellID)
	if shell == nil {
		return &agentdomain.ToolExecutionResult{
			ToolName: "KillShell",
			Success:  false,
			Error:    fmt.Sprintf("Shell not found: %s", shellID),
		}, nil
	}

	if err := t.shellService.CancelShell(shellID); err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName: "KillShell",
			Success:  false,
			Error:    fmt.Sprintf("Failed to cancel shell: %v", err),
		}, nil
	}

	result := map[string]any{
		"shell_id": shellID,
		"command":  shell.Command,
		"message":  fmt.Sprintf("Shell %s cancelled successfully", shellID),
	}

	return &agentdomain.ToolExecutionResult{
		ToolName: "KillShell",
		Success:  true,
		Data:     result,
	}, nil
}

// Validate validates the tool arguments
func (t *KillShellTool) Validate(args map[string]any) error {
	shellID, ok := args["shell_id"].(string)
	if !ok || shellID == "" {
		return fmt.Errorf("shell_id is required and must be a non-empty string")
	}

	return nil
}

// IsEnabled returns whether the tool is enabled
func (t *KillShellTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results for different contexts
func (t *KillShellTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterUI:
		return t.FormatForUI(result)
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *KillShellTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to kill shell"
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Shell cancelled"
	}

	shellID, _ := data["shell_id"].(string)
	return fmt.Sprintf("Cancelled shell %s", shellID)
}

// FormatForUI formats the result for UI display
func (t *KillShellTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	return t.FormatForLLM(result)
}

// FormatForLLM formats the result for LLM consumption
func (t *KillShellTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Shell cancelled successfully"
	}

	message, _ := data["message"].(string)
	return message
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *KillShellTool) ShouldAlwaysExpand() bool {
	return false
}

// ShouldCollapseArg determines if a specific argument should be collapsed in UI
func (t *KillShellTool) ShouldCollapseArg(key string) bool {
	return false
}
