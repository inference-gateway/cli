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

// MultiEditTool handles multiple exact string replacements in a single file atomically
type MultiEditTool struct {
	config   *config.Config
	enabled  bool
	registry ReadToolTracker
}

// NewMultiEditTool creates a new multi-edit tool
func NewMultiEditTool(cfg *config.Config) *MultiEditTool {
	return &MultiEditTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Edit.Enabled,
	}
}

// NewMultiEditToolWithRegistry creates a new multi-edit tool with a registry for read tracking
func NewMultiEditToolWithRegistry(cfg *config.Config, registry ReadToolTracker) *MultiEditTool {
	return &MultiEditTool{
		config:   cfg,
		enabled:  cfg.Tools.Enabled && cfg.Tools.Edit.Enabled,
		registry: registry,
	}
}

// Definition returns the tool definition for the LLM
func (t *MultiEditTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name: "MultiEdit",
		Description: `This is a tool for making multiple edits to a single file in one operation. It is built on top of the Edit tool and allows you to perform multiple find-and-replace operations efficiently. Prefer this tool over the Edit tool when you need to make multiple edits to the same file.

Before using this tool:

1. Use the Read tool to understand the file's contents and context
2. Verify the directory path is correct

To make multiple file edits, provide the following:
1. file_path: The absolute path to the file to modify (must be absolute, not relative)
2. edits: An array of edit operations to perform, where each edit contains:
   - old_string: The text to replace (must match the file contents exactly, including all whitespace and indentation)
   - new_string: The edited text to replace the old_string
   - replace_all: Replace all occurences of old_string. This parameter is optional and defaults to false.

IMPORTANT:
- All edits are applied in sequence, in the order they are provided
- Each edit operates on the result of the previous edit
- All edits must be valid for the operation to succeed - if any edit fails, none will be applied
- This tool is ideal when you need to make several changes to different parts of the same file
- For Jupyter notebooks (.ipynb files), use the NotebookEdit instead

CRITICAL REQUIREMENTS:
1. All edits follow the same requirements as the single Edit tool
2. The edits are atomic - either all succeed or none are applied
3. Plan your edits carefully to avoid conflicts between sequential operations

WARNING:
- The tool will fail if edits.old_string doesn't match the file contents exactly (including whitespace)
- The tool will fail if edits.old_string and edits.new_string are the same
- Since edits are applied in sequence, ensure that earlier edits don't affect the text that later edits are trying to find

When making edits:
- Ensure all edits result in idiomatic, correct code
- Do not leave the code in a broken state
- Always use absolute file paths (starting with /)
- Only use emojis if the user explicitly requests it. Avoid adding emojis to files unless asked.
- Use replace_all for replacing and renaming strings across the file. This parameter is useful if you want to rename a variable for instance.

If you want to create a new file, use:
- A new file path, including dir name if needed
- First edit: empty old_string and the new file's contents as new_string
- Subsequent edits: normal edit operations on the created content`,
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "The absolute path to the file to modify",
				},
				"edits": map[string]interface{}{
					"type":        "array",
					"description": "Array of edit operations to perform sequentially on the file",
					"minItems":    1,
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"old_string": map[string]interface{}{
								"type":        "string",
								"description": "The text to replace",
							},
							"new_string": map[string]interface{}{
								"type":        "string",
								"description": "The text to replace it with",
							},
							"replace_all": map[string]interface{}{
								"type":        "boolean",
								"description": "Replace all occurences of old_string (default false).",
								"default":     false,
							},
						},
						"required": []string{"old_string", "new_string"},
					},
				},
			},
			"required": []string{"file_path", "edits"},
		},
	}
}

// Execute runs the multi-edit tool with given arguments
func (t *MultiEditTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("multi-edit tool is not enabled")
	}

	if t.registry != nil && !t.registry.IsReadToolUsed() {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "MultiEdit tool requires that the Read tool has been used at least once in the conversation before editing files",
		}, nil
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path parameter is required and must be a string",
		}, nil
	}

	editsInterface, ok := args["edits"]
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "edits parameter is required",
		}, nil
	}

	editsArray, ok := editsInterface.([]interface{})
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "edits parameter must be an array",
		}, nil
	}

	if len(editsArray) == 0 {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "edits array must contain at least one edit operation",
		}, nil
	}

	// Parse and validate edits
	edits, err := t.parseEdits(editsArray)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	multiEditResult, err := t.executeMultiEdit(filePath, edits)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "MultiEdit",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      multiEditResult,
	}

	return result, nil
}

// EditOperation represents a single edit operation
type EditOperation struct {
	OldString  string
	NewString  string
	ReplaceAll bool
}

// parseEdits converts the interface{} array to EditOperation structs
func (t *MultiEditTool) parseEdits(editsArray []interface{}) ([]EditOperation, error) {
	var edits []EditOperation

	for i, editInterface := range editsArray {
		editMap, ok := editInterface.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("edit at index %d must be an object", i)
		}

		oldString, ok := editMap["old_string"].(string)
		if !ok {
			return nil, fmt.Errorf("old_string parameter is required and must be a string at edit index %d", i)
		}

		newString, ok := editMap["new_string"].(string)
		if !ok {
			return nil, fmt.Errorf("new_string parameter is required and must be a string at edit index %d", i)
		}

		if oldString == newString {
			return nil, fmt.Errorf("new_string must be different from old_string at edit index %d", i)
		}

		replaceAll := false
		if replaceAllVal, exists := editMap["replace_all"]; exists {
			if val, ok := replaceAllVal.(bool); ok {
				replaceAll = val
			}
		}

		edits = append(edits, EditOperation{
			OldString:  oldString,
			NewString:  newString,
			ReplaceAll: replaceAll,
		})
	}

	return edits, nil
}

// Validate checks if the multi-edit tool arguments are valid
func (t *MultiEditTool) Validate(args map[string]interface{}) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("multi-edit tool is not enabled")
	}

	if t.registry != nil && !t.registry.IsReadToolUsed() {
		return fmt.Errorf("multi-edit tool requires that the Read tool has been used at least once in the conversation before editing files")
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

	editsInterface, ok := args["edits"]
	if !ok {
		return fmt.Errorf("edits parameter is required")
	}

	editsArray, ok := editsInterface.([]interface{})
	if !ok {
		return fmt.Errorf("edits parameter must be an array")
	}

	if len(editsArray) == 0 {
		return fmt.Errorf("edits array must contain at least one edit operation")
	}

	// Validate each edit operation
	_, err := t.parseEdits(editsArray)
	if err != nil {
		return err
	}

	return nil
}

// IsEnabled returns whether the multi-edit tool is enabled
func (t *MultiEditTool) IsEnabled() bool {
	return t.enabled
}

// executeMultiEdit performs the actual multi-edit operation atomically
func (t *MultiEditTool) executeMultiEdit(filePath string, edits []EditOperation) (*domain.MultiEditToolResult, error) {
	if err := t.validateFile(filePath); err != nil {
		return nil, err
	}

	var originalContentStr string
	var originalSize int64

	// Check if file exists
	originalContent, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - start with empty content for new file creation
			originalContentStr = ""
			originalSize = 0
		} else {
			return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
	} else {
		originalContentStr = string(originalContent)
		originalSize = int64(len(originalContent))
	}

	currentContent := originalContentStr

	var editResults []domain.EditOperationResult
	successfulEdits := 0

	// Apply edits sequentially, rolling back if any fail
	for i, edit := range edits {
		if !strings.Contains(currentContent, edit.OldString) {
			// Rollback - don't modify the file and return error
			return nil, fmt.Errorf("edit %d failed: old_string '%s' not found in file %s (after previous edits)", i+1, edit.OldString, filePath)
		}

		var newContent string
		var replacedCount int

		if edit.ReplaceAll {
			newContent = strings.ReplaceAll(currentContent, edit.OldString, edit.NewString)
			replacedCount = strings.Count(currentContent, edit.OldString)
		} else {
			count := strings.Count(currentContent, edit.OldString)
			if count > 1 {
				// Rollback - don't modify the file and return error
				return nil, fmt.Errorf("edit %d failed: old_string '%s' is not unique in file %s (found %d occurrences after previous edits). Use replace_all=true to replace all occurrences or provide a larger string with more surrounding context to make it unique", i+1, edit.OldString, filePath, count)
			}
			newContent = strings.Replace(currentContent, edit.OldString, edit.NewString, 1)
			replacedCount = 1
		}

		// Update content for next iteration
		currentContent = newContent
		successfulEdits++

		editResults = append(editResults, domain.EditOperationResult{
			OldString:     edit.OldString,
			NewString:     edit.NewString,
			ReplaceAll:    edit.ReplaceAll,
			ReplacedCount: replacedCount,
			Success:       true,
		})
	}

	// All edits succeeded, write the file
	fileModified := false
	if currentContent != originalContentStr {
		if err := os.WriteFile(filePath, []byte(currentContent), 0644); err != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
		fileModified = true
	}

	newSize := int64(len(currentContent))
	bytesDifference := newSize - originalSize

	result := &domain.MultiEditToolResult{
		FilePath:        filePath,
		Edits:           editResults,
		TotalEdits:      len(edits),
		SuccessfulEdits: successfulEdits,
		FileModified:    fileModified,
		OriginalSize:    originalSize,
		NewSize:         newSize,
		BytesDifference: bytesDifference,
	}

	return result, nil
}

// validatePathSecurity checks if a path is allowed for editing (reuses the same logic as other tools)
func (t *MultiEditTool) validatePathSecurity(path string) error {
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

// validateFile checks if a file path is valid - supports both existing files and new file creation
func (t *MultiEditTool) validateFile(path string) error {
	if err := t.validatePathSecurity(path); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// For new file creation, just ensure the directory exists
			return nil
		}
		return fmt.Errorf("cannot access file %s: %w", path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("path %s is a directory, not a file", path)
	}

	return nil
}
