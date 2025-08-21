package tools

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// BashTool handles bash command execution with security validation
type BashTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
}

// NewBashTool creates a new bash tool
func NewBashTool(cfg *config.Config) *BashTool {
	return &BashTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Bash.Enabled,
		formatter: domain.NewBaseFormatter("Bash"),
	}
}

// Definition returns the tool definition for the LLM
func (t *BashTool) Definition() domain.ToolDefinition {
	var allowedCommands []string

	for _, cmd := range t.config.Tools.Bash.Whitelist.Commands {
		allowedCommands = append(allowedCommands, cmd)
		switch cmd {
		case "ls":
			allowedCommands = append(allowedCommands, "ls -l", "ls -la", "ls -a")
		case "git":
		case "grep":
			allowedCommands = append(allowedCommands, "grep -r", "grep -n", "grep -i")
		}
	}

	patternExamples := []string{
		"git status",
		"git log --oneline -n 5",
		"git log --oneline -n 10",
		"docker ps",
		"kubectl get pods",
	}
	allowedCommands = append(allowedCommands, patternExamples...)

	commandDescription := "The bash command to execute. Must be from the whitelist of allowed commands."
	if len(allowedCommands) > 0 {
		commandDescription += " Available commands include: " + strings.Join(allowedCommands, ", ")
	}

	return domain.ToolDefinition{
		Name:        "Bash",
		Description: "Execute whitelisted bash commands securely. Only pre-approved commands from the whitelist can be executed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": commandDescription,
					"enum":        allowedCommands,
				},
				"format": map[string]any{
					"type":        "string",
					"description": "Output format (text or json)",
					"enum":        []string{"text", "json"},
					"default":     "text",
				},
			},
			"required": []string{"command"},
		},
	}
}

// Execute runs the bash tool with given arguments
func (t *BashTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	command, ok := args["command"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Bash",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "command parameter is required and must be a string",
		}, nil
	}

	bashResult, err := t.executeBash(ctx, command)
	success := err == nil && bashResult.ExitCode == 0

	toolData := &domain.BashToolResult{
		Command:  bashResult.Command,
		Output:   bashResult.Output,
		Error:    bashResult.Error,
		ExitCode: bashResult.ExitCode,
		Duration: bashResult.Duration,
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Bash",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result, nil
}

// Validate checks if the bash tool arguments are valid
func (t *BashTool) Validate(args map[string]any) error {
	command, ok := args["command"].(string)
	if !ok {
		return fmt.Errorf("command parameter is required and must be a string")
	}

	if !t.isCommandAllowed(command) {
		return fmt.Errorf("command not whitelisted: %s", command)
	}

	return nil
}

// IsEnabled returns whether the bash tool is enabled
func (t *BashTool) IsEnabled() bool {
	return t.enabled
}

// BashResult represents the internal result of a bash command execution
type BashResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
}

// executeBash executes a bash command with security validation
func (t *BashTool) executeBash(ctx context.Context, command string) (*BashResult, error) {
	start := time.Now()
	result := &BashResult{
		Command: command,
	}

	if !t.isCommandAllowed(command) {
		result.ExitCode = -1
		result.Duration = time.Since(start).String()
		result.Error = fmt.Sprintf("command not whitelisted: %s", command)
		return result, fmt.Errorf("command not whitelisted: %s", command)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	output, err := cmd.CombinedOutput()
	result.Duration = time.Since(start).String()
	result.Output = string(output)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
		} else {
			result.ExitCode = -1
		}
		result.Error = err.Error()
	}

	return result, nil
}

// isCommandAllowed checks if a command is whitelisted
func (t *BashTool) isCommandAllowed(command string) bool {
	command = strings.TrimSpace(command)

	for _, allowed := range t.config.Tools.Bash.Whitelist.Commands {
		if command == allowed || strings.HasPrefix(command, allowed+" ") {
			return true
		}
	}

	for _, pattern := range t.config.Tools.Bash.Whitelist.Patterns {
		matched, err := regexp.MatchString(pattern, command)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// FormatResult formats tool execution results for different contexts
func (t *BashTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *BashTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	bashResult, ok := result.Data.(*domain.BashToolResult)
	if !ok {
		if result.Success {
			return "Execution completed successfully"
		}
		return "Execution failed"
	}

	if bashResult.ExitCode == 0 && bashResult.Output != "" {
		return t.formatter.TruncateText(strings.TrimSpace(bashResult.Output), 60)
	} else if bashResult.ExitCode != 0 {
		return fmt.Sprintf("Exit code: %d", bashResult.ExitCode)
	}
	return "Command completed"
}

// FormatForUI formats the result for UI display
func (t *BashTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, preview))

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *BashTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatBashData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatBashData formats bash-specific data
func (t *BashTool) formatBashData(data any) string {
	bashResult, ok := data.(*domain.BashToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Command: %s\n", bashResult.Command))
	output.WriteString(fmt.Sprintf("Exit Code: %d\n", bashResult.ExitCode))
	if bashResult.Error != "" {
		output.WriteString(fmt.Sprintf("Error: %s\n", bashResult.Error))
	}
	if bashResult.Output != "" {
		output.WriteString(fmt.Sprintf("Output:\n%s\n", bashResult.Output))
	}
	return output.String()
}
