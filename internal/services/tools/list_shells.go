package tools

import (
	"context"
	"fmt"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ListShellsTool implements listing of background shells
type ListShellsTool struct {
	enabled                bool
	backgroundShellService domain.BackgroundShellService
}

// NewListShellsTool creates a new ListShells tool
func NewListShellsTool(cfg *config.Config, shellService domain.BackgroundShellService) *ListShellsTool {
	return &ListShellsTool{
		enabled:                cfg.Tools.Enabled && cfg.Tools.Bash.BackgroundShells.Enabled,
		backgroundShellService: shellService,
	}
}

// Definition returns the tool definition for the LLM
func (t *ListShellsTool) Definition() sdk.ChatCompletionTool {
	description := "Lists all background shell processes currently running or recently completed. Shows shell ID, command, state, elapsed time, and output size for each shell. Use this to monitor background processes started with the Bash tool."

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ListShells",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type":                 "object",
				"properties":           map[string]any{},
				"required":             []string{},
				"additionalProperties": false,
			},
		},
	}
}

// Execute lists all background shells
func (t *ListShellsTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	shells := t.backgroundShellService.GetAllShells()

	if len(shells) == 0 {
		return &domain.ToolExecutionResult{
			ToolName:  "ListShells",
			Arguments: args,
			Success:   true,
			Data: map[string]any{
				"shell_count": 0,
				"message":     "No background shells are currently running or tracked.",
			},
		}, nil
	}

	shellInfos := make([]map[string]any, len(shells))
	for i, shell := range shells {
		info := domain.NewShellInfo(shell)
		shellInfos[i] = map[string]any{
			"shell_id":     info.ShellID,
			"command":      info.Command,
			"state":        info.State.String(),
			"started_at":   info.StartedAt.Format("15:04:05"),
			"elapsed":      info.Elapsed.String(),
			"output_size":  info.OutputSize,
			"exit_code":    info.ExitCode,
			"completed_at": info.CompletedAt,
		}
	}

	return &domain.ToolExecutionResult{
		ToolName:  "ListShells",
		Arguments: args,
		Success:   true,
		Data: map[string]any{
			"shell_count": len(shells),
			"shells":      shellInfos,
		},
	}, nil
}

// Validate validates the tool arguments (none required for ListShells)
func (t *ListShellsTool) Validate(args map[string]any) error {
	return nil
}

// IsEnabled returns whether the tool is enabled
func (t *ListShellsTool) IsEnabled() bool {
	return t.enabled && t.backgroundShellService != nil
}

// FormatResult formats the result for display
func (t *ListShellsTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	if formatType == domain.FormatterShort {
		if data, ok := result.Data.(map[string]any); ok {
			count := data["shell_count"].(int)
			if count == 0 {
				return "No background shells running"
			}
			return fmt.Sprintf("Found %d background shell(s)", count)
		}
	}

	if formatType == domain.FormatterLLM {
		return t.formatLLMResult(result)
	}

	if data, ok := result.Data.(map[string]any); ok {
		count := data["shell_count"].(int)
		if count == 0 {
			return "No background shells running"
		}

		shells := data["shells"].([]map[string]any)
		output := fmt.Sprintf("Background Shells (%d):\n\n", count)
		for _, shell := range shells {
			state := shell["state"].(string)
			var indicator string
			switch state {
			case "completed":
				indicator = "[OK]"
			case "failed":
				indicator = "[FAIL]"
			case "cancelled":
				indicator = "[STOP]"
			default:
				indicator = "[RUN]"
			}

			output += fmt.Sprintf("%s %s\n", indicator, shell["shell_id"])
			output += fmt.Sprintf("   %s\n", shell["command"])
			output += fmt.Sprintf("   %s | %s | %d bytes\n\n",
				shell["state"], shell["elapsed"], shell["output_size"])
		}
		return output
	}

	return "ListShells completed"
}

// FormatPreview returns a short preview
func (t *ListShellsTool) FormatPreview(result *domain.ToolExecutionResult) string {
	return t.FormatResult(result, domain.FormatterShort)
}

// ShouldCollapseArg returns whether an argument should be collapsed
func (t *ListShellsTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand returns whether results should always be expanded
func (t *ListShellsTool) ShouldAlwaysExpand() bool {
	return false
}

// formatLLMResult formats the result for LLM consumption
func (t *ListShellsTool) formatLLMResult(result *domain.ToolExecutionResult) string {
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "ListShells completed"
	}

	count := data["shell_count"].(int)
	if count == 0 {
		return "No background shells are currently running or tracked."
	}

	shells := data["shells"].([]map[string]any)
	output := fmt.Sprintf("Found %d background shell(s):\n\n", count)
	for i, shell := range shells {
		output += fmt.Sprintf("%d. Shell ID: %s\n", i+1, shell["shell_id"])
		output += fmt.Sprintf("   Command: %s\n", shell["command"])
		output += fmt.Sprintf("   State: %s\n", shell["state"])
		output += fmt.Sprintf("   Started: %s\n", shell["started_at"])
		output += fmt.Sprintf("   Elapsed: %s\n", shell["elapsed"])
		output += fmt.Sprintf("   Output Size: %d bytes\n", shell["output_size"])
		if shell["exit_code"] != nil {
			output += fmt.Sprintf("   Exit Code: %d\n", shell["exit_code"])
		}
		output += "\n"
	}
	output += "\nUse BashOutput(shell_id=\"<id>\") to view output or KillShell(shell_id=\"<id>\") to terminate a shell."
	return output
}
