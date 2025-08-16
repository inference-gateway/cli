package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// WriteTool handles file writing operations to the filesystem
type WriteTool struct {
	config  *config.Config
	enabled bool
}

// NewWriteTool creates a new write tool
func NewWriteTool(cfg *config.Config) *WriteTool {
	return &WriteTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Write.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *WriteTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "Write",
		Description: "Write content to a file on the filesystem. Creates parent directories if needed.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The content to write to the file",
				},
				"create_dirs": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to create parent directories if they don't exist",
					"default":     true,
				},
				"overwrite": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to overwrite existing files",
					"default":     true,
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Output format (text or json)",
					"enum":        []string{"text", "json"},
					"default":     "text",
				},
			},
			"required": []string{"file_path", "content"},
		},
	}
}

// Execute runs the write tool with given arguments
func (t *WriteTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("write tool is not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Write",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path parameter is required and must be a string",
		}, nil
	}

	content, ok := args["content"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Write",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "content parameter is required and must be a string",
		}, nil
	}

	createDirs := true
	if createDirsVal, exists := args["create_dirs"]; exists {
		if val, ok := createDirsVal.(bool); ok {
			createDirs = val
		}
	}

	overwrite := true
	if overwriteVal, exists := args["overwrite"]; exists {
		if val, ok := overwriteVal.(bool); ok {
			overwrite = val
		}
	}

	writeResult, err := t.executeWrite(filePath, content, createDirs, overwrite)
	if err != nil {
		return nil, err
	}

	var toolData *domain.FileWriteToolResult
	if writeResult != nil {
		toolData = &domain.FileWriteToolResult{
			FilePath:    writeResult.FilePath,
			BytesWriten: writeResult.BytesWriten,
			Created:     writeResult.Created,
			Overwritten: writeResult.Overwritten,
			DirsCreated: writeResult.DirsCreated,
			Error:       writeResult.Error,
		}
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Write",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	return result, nil
}

// Validate checks if the write tool arguments are valid
func (t *WriteTool) Validate(args map[string]interface{}) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("write tool is not enabled")
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

	if _, ok := args["content"].(string); !ok {
		return fmt.Errorf("content parameter is required and must be a string")
	}

	if createDirs, exists := args["create_dirs"]; exists {
		if _, ok := createDirs.(bool); !ok {
			return fmt.Errorf("create_dirs parameter must be a boolean")
		}
	}

	if overwrite, exists := args["overwrite"]; exists {
		if _, ok := overwrite.(bool); !ok {
			return fmt.Errorf("overwrite parameter must be a boolean")
		}
	}

	if format, ok := args["format"].(string); ok {
		if format != "text" && format != "json" {
			return fmt.Errorf("format must be 'text' or 'json'")
		}
	} else if args["format"] != nil {
		return fmt.Errorf("format parameter must be a string")
	}

	return nil
}

// IsEnabled returns whether the write tool is enabled
func (t *WriteTool) IsEnabled() bool {
	return t.enabled
}

// FileWriteResult represents the internal result of a file write operation
type FileWriteResult struct {
	FilePath    string `json:"file_path"`
	BytesWriten int64  `json:"bytes_written"`
	Created     bool   `json:"created"`
	Overwritten bool   `json:"overwritten"`
	DirsCreated bool   `json:"dirs_created"`
	Error       string `json:"error,omitempty"`
}

// executeWrite writes content to a file
func (t *WriteTool) executeWrite(filePath, content string, createDirs, overwrite bool) (*FileWriteResult, error) {
	result := &FileWriteResult{
		FilePath: filePath,
	}

	_, err := os.Stat(filePath)
	fileExists := err == nil
	result.Overwritten = fileExists && overwrite

	if fileExists && !overwrite {
		return nil, fmt.Errorf("file %s already exists and overwrite is false", filePath)
	}

	if err := t.createParentDirs(filePath, createDirs, result); err != nil {
		return nil, err
	}

	// Write the file
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	result.BytesWriten = int64(len(content))
	result.Created = !fileExists

	return result, nil
}

// createParentDirs creates parent directories if needed and allowed
func (t *WriteTool) createParentDirs(filePath string, createDirs bool, result *FileWriteResult) error {
	if !createDirs {
		return nil
	}

	dir := filepath.Dir(filePath)
	if dir == "." || dir == "/" {
		return nil
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directories for %s: %w", filePath, err)
		}
		result.DirsCreated = true
	}

	return nil
}

// validatePathSecurity checks if a path is allowed for writing (no file existence check)
func (t *WriteTool) validatePathSecurity(path string) error {
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
