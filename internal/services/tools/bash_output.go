package tools

import (
	"context"
	"fmt"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// BashOutputTool retrieves output from background bash shells
type BashOutputTool struct {
	config       *config.Config
	shellService domain.BackgroundShellService
	enabled      bool
}

// NewBashOutputTool creates a new BashOutput tool
func NewBashOutputTool(cfg *config.Config, shellService domain.BackgroundShellService) *BashOutputTool {
	return &BashOutputTool{
		config:       cfg,
		shellService: shellService,
		enabled:      cfg.Tools.Enabled && cfg.Tools.Bash.BackgroundShells.Enabled,
	}
}

// Definition returns the tool definition for the SDK
func (t *BashOutputTool) Definition() sdk.ChatCompletionTool {
	description := "Retrieves output from a running or completed background bash shell. Returns only new output since the last read. Use this to monitor long-running commands that were moved to the background."

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "BashOutput",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"bash_id": map[string]any{
						"type":        "string",
						"description": "The shell ID returned when the command was moved to background",
					},
					"filter": map[string]any{
						"type":        "string",
						"description": "Optional regex pattern to filter output lines. Only lines matching the pattern will be returned.",
					},
				},
				"required":             []string{"bash_id"},
				"additionalProperties": false,
			},
		},
	}
}

// Execute retrieves output from a background shell
func (t *BashOutputTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	// Validate arguments
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	bashID, _ := args["bash_id"].(string)
	filter, _ := args["filter"].(string)

	// Get shell output
	var output string
	var newOffset int64
	var state domain.ShellState
	var err error

	if filter != "" {
		output, newOffset, state, err = t.shellService.GetShellOutputWithFilter(bashID, -1, filter)
	} else {
		output, newOffset, state, err = t.shellService.GetShellOutput(bashID, -1)
	}

	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName: "BashOutput",
			Success:  false,
			Error:    fmt.Sprintf("Failed to get shell output: %v", err),
		}, nil
	}

	// Build response
	shell := t.shellService.GetShell(bashID)
	if shell == nil {
		return &domain.ToolExecutionResult{
			ToolName: "BashOutput",
			Success:  false,
			Error:    fmt.Sprintf("Shell not found: %s", bashID),
		}, nil
	}

	var statusMsg string
	switch state {
	case domain.ShellStateRunning:
		statusMsg = "Shell is still running"
	case domain.ShellStateCompleted:
		exitCode := 0
		if shell.ExitCode != nil {
			exitCode = *shell.ExitCode
		}
		statusMsg = fmt.Sprintf("Shell completed with exit code %d", exitCode)
	case domain.ShellStateFailed:
		exitCode := -1
		if shell.ExitCode != nil {
			exitCode = *shell.ExitCode
		}
		statusMsg = fmt.Sprintf("Shell failed with exit code %d", exitCode)
	case domain.ShellStateCancelled:
		statusMsg = "Shell was cancelled"
	}

	// Build result data
	result := map[string]any{
		"shell_id":       bashID,
		"command":        shell.Command,
		"state":          string(state),
		"status":         statusMsg,
		"output":         output,
		"output_bytes":   newOffset,
		"has_more":       state == domain.ShellStateRunning,
		"filter_applied": filter != "",
	}

	return &domain.ToolExecutionResult{
		ToolName: "BashOutput",
		Success:  true,
		Data:     result,
	}, nil
}

// Validate validates the tool arguments
func (t *BashOutputTool) Validate(args map[string]any) error {
	bashID, ok := args["bash_id"].(string)
	if !ok || bashID == "" {
		return fmt.Errorf("bash_id is required and must be a non-empty string")
	}

	// Validate filter pattern if provided
	if filter, ok := args["filter"].(string); ok && filter != "" {
		// The service will validate the regex pattern
		// We just check it's a string here
		if len(filter) > 1000 {
			return fmt.Errorf("filter pattern is too long (max 1000 characters)")
		}
	}

	return nil
}

// IsEnabled returns whether the tool is enabled
func (t *BashOutputTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results for different contexts
func (t *BashOutputTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *BashOutputTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to retrieve shell output"
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Retrieved shell output"
	}

	shellID, _ := data["shell_id"].(string)
	state, _ := data["state"].(string)
	hasMore, _ := data["has_more"].(bool)

	if hasMore {
		return fmt.Sprintf("Shell %s is still running", shellID)
	}
	return fmt.Sprintf("Shell %s %s", shellID, state)
}

// FormatForUI formats the result for UI display
func (t *BashOutputTool) FormatForUI(result *domain.ToolExecutionResult) string {
	return t.FormatForLLM(result)
}

// FormatForLLM formats the result for LLM consumption
func (t *BashOutputTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Retrieved shell output"
	}

	output, _ := data["output"].(string)
	status, _ := data["status"].(string)

	return fmt.Sprintf("%s\n\nOutput:\n%s", status, output)
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *BashOutputTool) ShouldAlwaysExpand() bool {
	return false
}

// ShouldCollapseArg determines if a specific argument should be collapsed in UI
func (t *BashOutputTool) ShouldCollapseArg(key string) bool {
	return false
}
