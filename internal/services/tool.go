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

// FileReadResult represents the result of a file read operation
type FileReadResult struct {
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Error     string `json:"error,omitempty"`
}

// LLMToolService implements ToolService with direct tool execution
type LLMToolService struct {
	config      *config.Config
	fileService domain.FileService
	enabled     bool
}

// NewLLMToolService creates a new LLM tool service
func NewLLMToolService(cfg *config.Config, fileService domain.FileService) *LLMToolService {
	return &LLMToolService{
		config:      cfg,
		fileService: fileService,
		enabled:     cfg.Tools.Enabled,
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
		{
			Name:        "Read",
			Description: "Read file content from the filesystem with optional line range",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to read",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "Starting line number (1-indexed, optional)",
						"minimum":     1,
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "Ending line number (1-indexed, optional)",
						"minimum":     1,
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (text or json)",
						"enum":        []string{"text", "json"},
						"default":     "text",
					},
				},
				"required": []string{"file_path"},
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
		return s.executeBashTool(ctx, args)
	case "Read":
		return s.executeReadTool(args)
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

	switch name {
	case "Bash":
		return s.validateBashTool(args)
	case "Read":
		return s.validateReadTool(args)
	default:
		return nil
	}
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

// executeRead reads a file with optional line range
func (s *LLMToolService) executeRead(filePath string, startLine, endLine int) (*FileReadResult, error) {
	result := &FileReadResult{
		FilePath:  filePath,
		StartLine: startLine,
		EndLine:   endLine,
	}

	var content string
	var err error

	if startLine > 0 || endLine > 0 {
		content, err = s.fileService.ReadFileLines(filePath, startLine, endLine)
	} else {
		content, err = s.fileService.ReadFile(filePath)
	}

	if err != nil {
		result.Error = err.Error()
		return result, nil
	}

	result.Content = content
	result.Size = int64(len(content))

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

// executeBashTool handles Bash tool execution
func (s *LLMToolService) executeBashTool(ctx context.Context, args map[string]interface{}) (string, error) {
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

	return s.formatBashResult(result), nil
}

// executeReadTool handles Read tool execution
func (s *LLMToolService) executeReadTool(args map[string]interface{}) (string, error) {
	filePath, ok := args["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required and must be a string")
	}

	format, ok := args["format"].(string)
	if !ok {
		format = "text"
	}

	var startLine, endLine int
	if startLineFloat, ok := args["start_line"].(float64); ok {
		startLine = int(startLineFloat)
	}
	if endLineFloat, ok := args["end_line"].(float64); ok {
		endLine = int(endLineFloat)
	}

	result, err := s.executeRead(filePath, startLine, endLine)
	if err != nil {
		return "", fmt.Errorf("file read failed: %w", err)
	}

	if format == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}
		return string(jsonOutput), nil
	}

	return s.formatReadResult(result), nil
}

// validateBashTool validates Bash tool arguments
func (s *LLMToolService) validateBashTool(args map[string]interface{}) error {
	command, ok := args["command"].(string)
	if !ok {
		return fmt.Errorf("command parameter is required and must be a string")
	}

	if !s.isCommandAllowed(command) {
		return fmt.Errorf("command not whitelisted: %s", command)
	}

	return nil
}

// validateReadTool validates Read tool arguments
func (s *LLMToolService) validateReadTool(args map[string]interface{}) error {
	filePath, ok := args["file_path"].(string)
	if !ok {
		return fmt.Errorf("file_path parameter is required and must be a string")
	}

	if err := s.fileService.ValidateFile(filePath); err != nil {
		return fmt.Errorf("file validation failed: %w", err)
	}

	return s.validateLineNumbers(args)
}

// validateLineNumbers validates start_line and end_line parameters
func (s *LLMToolService) validateLineNumbers(args map[string]interface{}) error {
	var startLine float64
	var hasStartLine bool

	if startLineFloat, ok := args["start_line"].(float64); ok {
		if startLineFloat < 1 {
			return fmt.Errorf("start_line must be >= 1")
		}
		startLine = startLineFloat
		hasStartLine = true
	}

	if endLineFloat, ok := args["end_line"].(float64); ok {
		if endLineFloat < 1 {
			return fmt.Errorf("end_line must be >= 1")
		}
		if hasStartLine && endLineFloat < startLine {
			return fmt.Errorf("end_line must be >= start_line")
		}
	}

	return nil
}

// formatBashResult formats bash execution result for text output
func (s *LLMToolService) formatBashResult(result *ToolResult) string {
	output := fmt.Sprintf("Command: %s\n", result.Command)
	output += fmt.Sprintf("Exit Code: %d\n", result.ExitCode)
	output += fmt.Sprintf("Duration: %s\n", result.Duration)

	if result.Error != "" {
		output += fmt.Sprintf("Error: %s\n", result.Error)
	}

	output += fmt.Sprintf("Output:\n%s", result.Output)
	return output
}

// formatReadResult formats read result for text output
func (s *LLMToolService) formatReadResult(result *FileReadResult) string {
	output := fmt.Sprintf("File: %s\n", result.FilePath)
	if result.StartLine > 0 {
		output += fmt.Sprintf("Lines: %d", result.StartLine)
		if result.EndLine > 0 && result.EndLine != result.StartLine {
			output += fmt.Sprintf("-%d", result.EndLine)
		}
		output += "\n"
	}
	output += fmt.Sprintf("Size: %d bytes\n", result.Size)
	if result.Error != "" {
		output += fmt.Sprintf("Error: %s\n", result.Error)
	}
	output += fmt.Sprintf("Content:\n%s", result.Content)
	return output
}
