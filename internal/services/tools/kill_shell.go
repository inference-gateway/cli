package tools

import (
	"context"
	"fmt"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// KillShellTool cancels a running background shell
type KillShellTool struct {
	config       *config.Config
	shellService domain.BackgroundShellService
	enabled      bool
}

// NewKillShellTool creates a new KillShell tool
func NewKillShellTool(cfg *config.Config, shellService domain.BackgroundShellService) *KillShellTool {
	return &KillShellTool{
		config:       cfg,
		shellService: shellService,
		enabled:      cfg.Tools.Enabled && cfg.Tools.Bash.BackgroundShells.Enabled,
	}
}

// Definition returns the tool definition for the SDK
func (t *KillShellTool) Definition() sdk.ChatCompletionTool {
	description := "Kills a running background bash shell by its ID. Sends SIGTERM first, then SIGKILL if needed after 5 seconds."

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
func (t *KillShellTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	// Validate arguments
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	shellID, _ := args["shell_id"].(string)

	// Get shell info before cancelling
	shell := t.shellService.GetShell(shellID)
	if shell == nil {
		return &domain.ToolExecutionResult{
			ToolName: "KillShell",
			Success:  false,
			Error:    fmt.Sprintf("Shell not found: %s", shellID),
		}, nil
	}

	// Cancel the shell
	if err := t.shellService.CancelShell(shellID); err != nil {
		return &domain.ToolExecutionResult{
			ToolName: "KillShell",
			Success:  false,
			Error:    fmt.Sprintf("Failed to cancel shell: %v", err),
		}, nil
	}

	// Build result
	result := map[string]any{
		"shell_id": shellID,
		"command":  shell.Command,
		"message":  fmt.Sprintf("Shell %s cancelled successfully", shellID),
	}

	return &domain.ToolExecutionResult{
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
func (t *KillShellTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *KillShellTool) FormatPreview(result *domain.ToolExecutionResult) string {
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
func (t *KillShellTool) FormatForUI(result *domain.ToolExecutionResult) string {
	return t.FormatForLLM(result)
}

// FormatForLLM formats the result for LLM consumption
func (t *KillShellTool) FormatForLLM(result *domain.ToolExecutionResult) string {
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
