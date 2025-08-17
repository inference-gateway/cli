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

// EditTool handles exact string replacements in files with strict safety rules
type EditTool struct {
	config   *config.Config
	enabled  bool
	registry ReadToolTracker
}

// ReadToolTracker interface for tracking read tool usage
type ReadToolTracker interface {
	IsReadToolUsed() bool
}

// NewEditTool creates a new edit tool
func NewEditTool(cfg *config.Config) *EditTool {
	return &EditTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Edit.Enabled,
	}
}

// NewEditToolWithRegistry creates a new edit tool with a registry for read tracking
func NewEditToolWithRegistry(cfg *config.Config, registry ReadToolTracker) *EditTool {
	return &EditTool{
		config:   cfg,
		enabled:  cfg.Tools.Enabled && cfg.Tools.Edit.Enabled,
		registry: registry,
	}
}

// Definition returns the tool definition for the LLM
func (t *EditTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name: "Edit",
		Description: `Performs exact string replacements in files.

Usage:
- You must use your Read tool at least once in the conversation before editing. This tool will error if you attempt an edit without reading the file.
- When editing text from Read tool output, ensure you preserve the exact indentation (tabs/spaces) as it appears AFTER the line number prefix. The line number prefix format is: spaces + line number + tab. Everything after that tab is the actual file content to match. Never include any part of the line number prefix in the old_string or new_string.
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- Only use emojis if the user explicitly requests it. Avoid adding emojis to files unless asked.
- The edit will FAIL if old_string is not unique in the file. Either provide a larger string with more surrounding context to make it unique or use replace_all to change every instance of old_string.
- Use replace_all for replacing and renaming strings across the file. This parameter is useful if you want to rename a variable for instance.`,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "The absolute path to the file to modify",
				},
				"old_string": map[string]interface{}{
					"type":        "string",
					"description": "The text to replace",
				},
				"new_string": map[string]interface{}{
					"type":        "string",
					"description": "The text to replace it with (must be different from old_string)",
				},
				"replace_all": map[string]interface{}{
					"type":        "boolean",
					"description": "Replace all occurrences of old_string (default false)",
					"default":     false,
				},
			},
			"required": []string{"file_path", "old_string", "new_string"},
		},
	}
}

// Execute runs the edit tool with given arguments
func (t *EditTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("edit tool is not enabled")
	}

	if t.registry != nil && !t.registry.IsReadToolUsed() {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "Edit tool requires that the Read tool has been used at least once in the conversation before editing files",
		}, nil
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path parameter is required and must be a string",
		}, nil
	}

	oldString, ok := args["old_string"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "old_string parameter is required and must be a string",
		}, nil
	}

	newString, ok := args["new_string"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "new_string parameter is required and must be a string",
		}, nil
	}

	if oldString == newString {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "new_string must be different from old_string",
		}, nil
	}

	replaceAll := false
	if replaceAllVal, exists := args["replace_all"]; exists {
		if val, ok := replaceAllVal.(bool); ok {
			replaceAll = val
		}
	}

	editResult, err := t.executeEdit(filePath, oldString, newString, replaceAll)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Edit",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      editResult,
	}

	return result, nil
}

// Validate checks if the edit tool arguments are valid
func (t *EditTool) Validate(args map[string]interface{}) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("edit tool is not enabled")
	}

	if t.registry != nil && !t.registry.IsReadToolUsed() {
		return fmt.Errorf("edit tool requires that the Read tool has been used at least once in the conversation before editing files")
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

	oldString, ok := args["old_string"].(string)
	if !ok {
		return fmt.Errorf("old_string parameter is required and must be a string")
	}

	if oldString == "" {
		return fmt.Errorf("old_string cannot be empty")
	}

	newString, ok := args["new_string"].(string)
	if !ok {
		return fmt.Errorf("new_string parameter is required and must be a string")
	}

	if oldString == newString {
		return fmt.Errorf("new_string must be different from old_string")
	}

	if replaceAll, exists := args["replace_all"]; exists {
		if _, ok := replaceAll.(bool); !ok {
			return fmt.Errorf("replace_all parameter must be a boolean")
		}
	}

	return nil
}

// IsEnabled returns whether the edit tool is enabled
func (t *EditTool) IsEnabled() bool {
	return t.enabled
}

// executeEdit performs the actual edit operation
func (t *EditTool) executeEdit(filePath, oldString, newString string, replaceAll bool) (*domain.EditToolResult, error) {
	if err := t.validateFile(filePath); err != nil {
		return nil, err
	}

	originalContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	originalContentStr := string(originalContent)
	originalSize := int64(len(originalContent))

	if !strings.Contains(originalContentStr, oldString) {
		return nil, fmt.Errorf("old_string not found in file %s", filePath)
	}

	var newContent string
	var replacedCount int

	if replaceAll {
		newContent = strings.ReplaceAll(originalContentStr, oldString, newString)
		replacedCount = strings.Count(originalContentStr, oldString)
	} else {
		count := strings.Count(originalContentStr, oldString)
		if count > 1 {
			return nil, fmt.Errorf("old_string '%s' is not unique in file %s (found %d occurrences). Use replace_all=true to replace all occurrences or provide a larger string with more surrounding context to make it unique", oldString, filePath, count)
		}
		newContent = strings.Replace(originalContentStr, oldString, newString, 1)
		replacedCount = 1
	}

	fileModified := false
	if newContent != originalContentStr {
		if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
		fileModified = true
	}

	newSize := int64(len(newContent))
	bytesDifference := newSize - originalSize

	result := &domain.EditToolResult{
		FilePath:        filePath,
		OldString:       oldString,
		NewString:       newString,
		ReplacedCount:   replacedCount,
		ReplaceAll:      replaceAll,
		FileModified:    fileModified,
		OriginalSize:    originalSize,
		NewSize:         newSize,
		BytesDifference: bytesDifference,
	}

	return result, nil
}

// validatePathSecurity checks if a path is allowed for editing (reuses the same logic as other tools)
func (t *EditTool) validatePathSecurity(path string) error {
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

// validateFile checks if a file path is valid and exists (only works with existing files)
func (t *EditTool) validateFile(path string) error {
	if err := t.validatePathSecurity(path); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file %s does not exist. Edit tool only works with existing files", path)
		}
		return fmt.Errorf("cannot access file %s: %w", path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("path %s is a directory, not a file", path)
	}

	return nil
}
