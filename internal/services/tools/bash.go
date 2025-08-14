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
	config  *config.Config
	enabled bool
}

// NewBashTool creates a new bash tool
func NewBashTool(cfg *config.Config) *BashTool {
	return &BashTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Bash.Enabled,
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
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": commandDescription,
					"enum":        allowedCommands,
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
	}
}

// Execute runs the bash tool with given arguments
func (t *BashTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
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
func (t *BashTool) Validate(args map[string]interface{}) error {
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
