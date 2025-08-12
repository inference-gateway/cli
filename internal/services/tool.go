package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
}

// LLMToolService implements ToolService with direct tool execution
type LLMToolService struct {
	config  *config.Config
	enabled bool
}

// NewLLMToolService creates a new LLM tool service
func NewLLMToolService(cfg *config.Config) *LLMToolService {
	return &LLMToolService{
		config:  cfg,
		enabled: cfg.Tools.Enabled,
	}
}

func (s *LLMToolService) ListTools() []domain.ToolDefinition {
	if !s.enabled {
		return []domain.ToolDefinition{}
	}

	return []domain.ToolDefinition{
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

func (s *LLMToolService) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	if !s.enabled {
		return "", fmt.Errorf("tools are not enabled")
	}

	switch name {
	case "Bash":
		command, ok := args["command"].(string)
		if !ok {
			return "", fmt.Errorf("command parameter is required and must be a string")
		}

		format, ok := args["format"].(string)
		if !ok {
			format = "text"
		}

		result, err := s.executeBash(ctx, command)
		if err != nil {
			return "", fmt.Errorf("bash execution failed: %w", err)
		}

		if format == "json" {
			jsonOutput, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal result: %w", err)
			}
			return string(jsonOutput), nil
		}

		output := fmt.Sprintf("Command: %s\n", result.Command)
		output += fmt.Sprintf("Exit Code: %d\n", result.ExitCode)
		output += fmt.Sprintf("Duration: %s\n", result.Duration)

		if result.Error != "" {
			output += fmt.Sprintf("Error: %s\n", result.Error)
		}

		output += fmt.Sprintf("Output:\n%s", result.Output)
		return output, nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *LLMToolService) IsToolEnabled(name string) bool {
	if !s.enabled {
		return false
	}

	tools := s.ListTools()
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func (s *LLMToolService) ValidateTool(name string, args map[string]interface{}) error {
	if !s.enabled {
		return fmt.Errorf("tools are not enabled")
	}

	if !s.IsToolEnabled(name) {
		return fmt.Errorf("tool '%s' is not available", name)
	}

	if name == "Bash" {
		command, ok := args["command"].(string)
		if !ok {
			return fmt.Errorf("command parameter is required and must be a string")
		}

		if !s.isCommandAllowed(command) {
			return fmt.Errorf("command not whitelisted: %s", command)
		}
	}

	return nil
}

// executeBash executes a bash command with security validation
func (s *LLMToolService) executeBash(ctx context.Context, command string) (*ToolResult, error) {
	if !s.isCommandAllowed(command) {
		return nil, fmt.Errorf("command not whitelisted: %s", command)
	}

	start := time.Now()
	result := &ToolResult{
		Command: command,
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
func (s *LLMToolService) isCommandAllowed(command string) bool {
	command = strings.TrimSpace(command)

	for _, allowed := range s.config.Tools.Whitelist.Commands {
		if command == allowed || strings.HasPrefix(command, allowed+" ") {
			return true
		}
	}

	for _, pattern := range s.config.Tools.Whitelist.Patterns {
		matched, err := regexp.MatchString(pattern, command)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// NoOpToolService implements ToolService as a no-op (when tools are disabled)
type NoOpToolService struct{}

// NewNoOpToolService creates a new no-op tool service
func NewNoOpToolService() *NoOpToolService {
	return &NoOpToolService{}
}

func (s *NoOpToolService) ListTools() []domain.ToolDefinition {
	return []domain.ToolDefinition{}
}

func (s *NoOpToolService) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("tools are not enabled")
}

func (s *NoOpToolService) IsToolEnabled(name string) bool {
	return false
}

func (s *NoOpToolService) ValidateTool(name string, args map[string]interface{}) error {
	return fmt.Errorf("tools are not enabled")
}
