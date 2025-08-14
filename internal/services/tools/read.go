package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// ReadTool handles file reading operations with optional line range
type ReadTool struct {
	config      *config.Config
	fileService domain.FileService
	enabled     bool
}

// NewReadTool creates a new read tool
func NewReadTool(cfg *config.Config, fileService domain.FileService) *ReadTool {
	return &ReadTool{
		config:      cfg,
		fileService: fileService,
		enabled:     cfg.Tools.Enabled,
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
	success := err == nil

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
		Success:   success,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result, nil
}

// Validate checks if the read tool arguments are valid
func (t *ReadTool) Validate(args map[string]interface{}) error {
	filePath, ok := args["file_path"].(string)
	if !ok {
		return fmt.Errorf("file_path parameter is required and must be a string")
	}

	if err := t.fileService.ValidateFile(filePath); err != nil {
		return fmt.Errorf("file validation failed: %w", err)
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
		content, err = t.fileService.ReadFileLines(filePath, startLine, endLine)
	} else {
		content, err = t.fileService.ReadFile(filePath)
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