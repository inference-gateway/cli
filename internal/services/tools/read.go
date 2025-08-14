package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// ReadTool handles file reading operations with optional line range
type ReadTool struct {
	config  *config.Config
	enabled bool
}

// NewReadTool creates a new read tool
func NewReadTool(cfg *config.Config) *ReadTool {
	return &ReadTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Read.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *ReadTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
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
	}
}

// Execute runs the read tool with given arguments
func (t *ReadTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("read tool is not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Read",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path parameter is required and must be a string",
		}, nil
	}

	var startLine, endLine int
	if startLineFloat, ok := args["start_line"].(float64); ok {
		startLine = int(startLineFloat)
	}
	if endLineFloat, ok := args["end_line"].(float64); ok {
		endLine = int(endLineFloat)
	}

	readResult, err := t.executeRead(filePath, startLine, endLine)
	if err != nil {
		return nil, err
	}

	var toolData *domain.FileReadToolResult
	if readResult != nil {
		toolData = &domain.FileReadToolResult{
			FilePath:  readResult.FilePath,
			Content:   readResult.Content,
			Size:      readResult.Size,
			StartLine: readResult.StartLine,
			EndLine:   readResult.EndLine,
			Error:     readResult.Error,
		}
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Read",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	return result, nil
}

// Validate checks if the read tool arguments are valid
func (t *ReadTool) Validate(args map[string]interface{}) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("read tool is not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return fmt.Errorf("file_path parameter is required and must be a string")
	}

	if filePath == "" {
		return fmt.Errorf("file_path cannot be empty")
	}

	if err := t.validatePathSecurity(filePath); err != nil {
		return err
	}

	if format, ok := args["format"].(string); ok {
		if format != "text" && format != "json" {
			return fmt.Errorf("format must be 'text' or 'json'")
		}
	} else if args["format"] != nil {
		return fmt.Errorf("format parameter must be a string")
	}

	return t.validateLineNumbers(args)
}

// IsEnabled returns whether the read tool is enabled
func (t *ReadTool) IsEnabled() bool {
	return t.enabled
}

// FileReadResult represents the internal result of a file read operation
type FileReadResult struct {
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Error     string `json:"error,omitempty"`
}

// executeRead reads a file with optional line range
func (t *ReadTool) executeRead(filePath string, startLine, endLine int) (*FileReadResult, error) {
	result := &FileReadResult{
		FilePath:  filePath,
		StartLine: startLine,
		EndLine:   endLine,
	}

	var content string
	var err error

	if startLine > 0 || endLine > 0 {
		content, err = t.readFileLines(filePath, startLine, endLine)
	} else {
		content, err = t.readFile(filePath)
	}

	if err != nil {
		return nil, err
	}

	result.Content = content
	result.Size = int64(len(content))

	return result, nil
}

// validateLineNumbers validates start_line and end_line parameters
func (t *ReadTool) validateLineNumbers(args map[string]interface{}) error {
	startLine, hasStartLine, err := t.validateSingleLineNumber(args, "start_line")
	if err != nil {
		return err
	}

	endLine, hasEndLine, err := t.validateSingleLineNumber(args, "end_line")
	if err != nil {
		return err
	}

	if hasStartLine && hasEndLine && endLine < startLine {
		return fmt.Errorf("end_line must be >= start_line")
	}

	return nil
}

// validateSingleLineNumber validates a single line number parameter
func (t *ReadTool) validateSingleLineNumber(args map[string]interface{}, paramName string) (float64, bool, error) {
	if args[paramName] == nil {
		return 0, false, nil
	}

	if lineFloat, ok := args[paramName].(float64); ok {
		if lineFloat < 1 {
			return 0, false, fmt.Errorf("%s must be >= 1", paramName)
		}
		return lineFloat, true, nil
	}

	if lineInt, ok := args[paramName].(int); ok {
		if lineInt < 1 {
			return 0, false, fmt.Errorf("%s must be >= 1", paramName)
		}
		return float64(lineInt), true, nil
	}

	return 0, false, fmt.Errorf("%s must be a number", paramName)
}

// validatePathSecurity checks if a path is allowed (no file existence check)
func (t *ReadTool) validatePathSecurity(path string) error {
	for _, excludePath := range t.config.Tools.ExcludePaths {
		if strings.HasPrefix(path, excludePath) {
			return fmt.Errorf("access to path '%s' is excluded for security", path)
		}

		if strings.Contains(excludePath, "*") && matchesPattern(path, excludePath) {
			return fmt.Errorf("access to path '%s' is excluded for security", path)
		}
	}
	return nil
}

// matchesPattern checks if a path matches a simple glob pattern
func matchesPattern(path, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix)
	}
	if strings.HasPrefix(pattern, "*") {
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(path, suffix)
	}
	return path == pattern
}

// validateFile checks if a file path is valid and readable
func (t *ReadTool) validateFile(path string) error {
	if err := t.validatePathSecurity(path); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file %s does not exist", path)
		}
		return fmt.Errorf("cannot access file %s: %w", path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("path %s is a directory, not a file", path)
	}

	return nil
}

// readFile reads the entire content of a file
func (t *ReadTool) readFile(path string) (string, error) {
	if err := t.validateFile(path); err != nil {
		return "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}

	return string(data), nil
}

// readFileLines reads specific lines from a file
func (t *ReadTool) readFileLines(path string, startLine, endLine int) (string, error) {
	content, err := t.readFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(content, "\n")

	if startLine < 1 {
		startLine = 1
	}
	if endLine < 1 || endLine > len(lines) {
		endLine = len(lines)
	}
	if startLine > endLine {
		return "", fmt.Errorf("start line %d is greater than end line %d", startLine, endLine)
	}

	selectedLines := lines[startLine-1:endLine]
	return strings.Join(selectedLines, "\n"), nil
}
