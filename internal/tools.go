package internal

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
)

// ToolEngine manages tool execution with security controls
type ToolEngine struct {
	config *config.Config
}

// NewToolEngine creates a new tool engine
func NewToolEngine(cfg *config.Config) *ToolEngine {
	return &ToolEngine{config: cfg}
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
}

// ExecuteBash executes a bash command with security validation
func (te *ToolEngine) ExecuteBash(command string) (*ToolResult, error) {
	if !te.config.Tools.Enabled {
		return nil, fmt.Errorf("tools are disabled in configuration")
	}

	if !te.isCommandAllowed(command) {
		return nil, fmt.Errorf("command not whitelisted: %s", command)
	}

	start := time.Now()
	result := &ToolResult{
		Command: command,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
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
func (te *ToolEngine) isCommandAllowed(command string) bool {
	command = strings.TrimSpace(command)

	for _, allowed := range te.config.Tools.Whitelist.Commands {
		if command == allowed || strings.HasPrefix(command, allowed+" ") {
			return true
		}
	}

	for _, pattern := range te.config.Tools.Whitelist.Patterns {
		matched, err := regexp.MatchString(pattern, command)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// ListAllowedCommands returns the list of whitelisted commands
func (te *ToolEngine) ListAllowedCommands() []string {
	var allowed []string
	allowed = append(allowed, te.config.Tools.Whitelist.Commands...)
	allowed = append(allowed, te.config.Tools.Whitelist.Patterns...)
	return allowed
}

// ValidateCommand checks if a command would be allowed without executing it
func (te *ToolEngine) ValidateCommand(command string) error {
	if !te.config.Tools.Enabled {
		return fmt.Errorf("tools are disabled")
	}

	if !te.isCommandAllowed(command) {
		return fmt.Errorf("command not whitelisted: %s", command)
	}

	return nil
}
