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

// DeleteTool handles file and directory deletion operations
type DeleteTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
}

// NewDeleteTool creates a new delete tool
func NewDeleteTool(cfg *config.Config) *DeleteTool {
	return &DeleteTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Delete.Enabled,
		formatter: domain.NewBaseFormatter("Delete"),
	}
}

// Definition returns the tool definition for the LLM
func (t *DeleteTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "Delete",
		Description: "Delete files or directories from the filesystem. Supports wildcard patterns for batch operations. Restricted to current working directory for security.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "The path to the file or directory to delete. Supports wildcard patterns like '*.txt' or 'temp/*' when wildcards are enabled.",
				},
				"recursive": map[string]any{
					"type":        "boolean",
					"description": "Whether to delete directories recursively",
					"default":     false,
				},
				"force": map[string]any{
					"type":        "boolean",
					"description": "Whether to force deletion (ignore non-existent files)",
					"default":     false,
				},
				"format": map[string]any{
					"type":        "string",
					"description": "Output format (text or json)",
					"enum":        []string{"text", "json"},
					"default":     "text",
				},
			},
			"required": []string{"path"},
		},
	}
}

// Execute runs the delete tool with given arguments
func (t *DeleteTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("delete tool is not enabled")
	}

	path, ok := args["path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Delete",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "path parameter is required and must be a string",
		}, nil
	}

	recursive := false
	if recursiveVal, exists := args["recursive"]; exists {
		if val, ok := recursiveVal.(bool); ok {
			recursive = val
		}
	}

	force := false
	if forceVal, exists := args["force"]; exists {
		if val, ok := forceVal.(bool); ok {
			force = val
		}
	}

	deleteResult, err := t.executeDelete(path, recursive, force)
	if err != nil {
		return nil, err
	}

	var toolData *domain.DeleteToolResult
	if deleteResult != nil {
		toolData = &domain.DeleteToolResult{
			Path:              deleteResult.Path,
			DeletedFiles:      deleteResult.DeletedFiles,
			DeletedDirs:       deleteResult.DeletedDirs,
			TotalFilesDeleted: deleteResult.TotalFilesDeleted,
			TotalDirsDeleted:  deleteResult.TotalDirsDeleted,
			WildcardExpanded:  deleteResult.WildcardExpanded,
			Errors:            deleteResult.Errors,
		}
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Delete",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	return result, nil
}

// Validate checks if the delete tool arguments are valid
func (t *DeleteTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("delete tool is not enabled")
	}

	path, ok := args["path"].(string)
	if !ok {
		return fmt.Errorf("path parameter is required and must be a string")
	}

	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	if err := t.validatePathSecurity(path); err != nil {
		return err
	}

	if recursive, exists := args["recursive"]; exists {
		if _, ok := recursive.(bool); !ok {
			return fmt.Errorf("recursive parameter must be a boolean")
		}
	}

	if force, exists := args["force"]; exists {
		if _, ok := force.(bool); !ok {
			return fmt.Errorf("force parameter must be a boolean")
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

// IsEnabled returns whether the delete tool is enabled
func (t *DeleteTool) IsEnabled() bool {
	return t.enabled
}

// DeleteResult represents the result of a delete operation
type DeleteResult struct {
	Path              string   `json:"path"`
	DeletedFiles      []string `json:"deleted_files"`
	DeletedDirs       []string `json:"deleted_dirs"`
	TotalFilesDeleted int      `json:"total_files_deleted"`
	TotalDirsDeleted  int      `json:"total_dirs_deleted"`
	WildcardExpanded  bool     `json:"wildcard_expanded"`
	Errors            []string `json:"errors,omitempty"`
}

// executeDelete performs the actual deletion operation
func (t *DeleteTool) executeDelete(path string, recursive, force bool) (*DeleteResult, error) {
	result := &DeleteResult{
		Path:         path,
		DeletedFiles: []string{},
		DeletedDirs:  []string{},
		Errors:       []string{},
	}

	if t.containsWildcards(path) {
		result.WildcardExpanded = true
		return t.executeWildcardDelete(path, recursive, force, result)
	}

	return t.executeSingleDelete(path, recursive, force, result)
}

// containsWildcards checks if a path contains wildcard characters
func (t *DeleteTool) containsWildcards(path string) bool {
	return strings.Contains(path, "*") || strings.Contains(path, "?") || strings.Contains(path, "[")
}

// executeWildcardDelete handles deletion with wildcard patterns
func (t *DeleteTool) executeWildcardDelete(pattern string, recursive, force bool, result *DeleteResult) (*DeleteResult, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid wildcard pattern: %w", err)
	}

	if len(matches) == 0 {
		if !force {
			return nil, fmt.Errorf("no files or directories match the pattern: %s", pattern)
		}
		return result, nil
	}

	for _, match := range matches {
		if err := t.validatePathSecurity(match); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Path %s: %v", match, err))
			continue
		}

		if err := t.deleteSinglePath(match, recursive, result); err != nil {
			if !force {
				return nil, err
			}
			result.Errors = append(result.Errors, fmt.Sprintf("Path %s: %v", match, err))
		}
	}

	return result, nil
}

// executeSingleDelete handles deletion of a single file or directory
func (t *DeleteTool) executeSingleDelete(path string, recursive, force bool, result *DeleteResult) (*DeleteResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) && force {
			return result, nil
		}
		return nil, fmt.Errorf("failed to stat path %s: %w", path, err)
	}

	if info.IsDir() && !recursive {
		return nil, fmt.Errorf("path %s is a directory, use recursive=true to delete directories", path)
	}

	if err := t.deleteSinglePath(path, recursive, result); err != nil {
		return nil, err
	}

	return result, nil
}

// deleteSinglePath deletes a single file or directory and updates the result
func (t *DeleteTool) deleteSinglePath(path string, recursive bool, result *DeleteResult) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat path %s: %w", path, err)
	}

	if info.IsDir() {
		return t.deleteDirectory(path, recursive, result)
	}

	return t.deleteFile(path, result)
}

// deleteDirectory handles directory deletion with recursive option
func (t *DeleteTool) deleteDirectory(path string, recursive bool, result *DeleteResult) error {
	if !recursive {
		return fmt.Errorf("path %s is a directory, use recursive=true to delete directories", path)
	}

	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("failed to delete directory %s: %w", path, err)
	}

	result.DeletedDirs = append(result.DeletedDirs, path)
	result.TotalDirsDeleted++
	return nil
}

// deleteFile handles single file deletion
func (t *DeleteTool) deleteFile(path string, result *DeleteResult) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete file %s: %w", path, err)
	}

	result.DeletedFiles = append(result.DeletedFiles, path)
	result.TotalFilesDeleted++
	return nil
}

// validatePathSecurity checks if a path is allowed for deletion within the sandbox
func (t *DeleteTool) validatePathSecurity(path string) error {
	return t.config.ValidatePathInSandbox(path)
}

// FormatResult formats tool execution results for different contexts
func (t *DeleteTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *DeleteTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	deleteResult, ok := result.Data.(*domain.DeleteToolResult)
	if !ok {
		if result.Success {
			return "Deletion completed successfully"
		}
		return "Deletion failed"
	}

	totalItems := deleteResult.TotalFilesDeleted + deleteResult.TotalDirsDeleted
	if totalItems == 0 {
		return "No items to delete"
	}

	var parts []string
	if deleteResult.TotalFilesDeleted > 0 {
		parts = append(parts, fmt.Sprintf("%d file%s", deleteResult.TotalFilesDeleted,
			t.pluralize(deleteResult.TotalFilesDeleted)))
	}
	if deleteResult.TotalDirsDeleted > 0 {
		parts = append(parts, fmt.Sprintf("%d director%s", deleteResult.TotalDirsDeleted,
			t.pluralizeDir(deleteResult.TotalDirsDeleted)))
	}

	action := "Deleted"
	if deleteResult.WildcardExpanded {
		action = "Deleted (wildcard)"
	}

	return fmt.Sprintf("%s %s", action, strings.Join(parts, " and "))
}

// FormatForUI formats the result for UI display
func (t *DeleteTool) FormatForUI(result *domain.ToolExecutionResult) string {
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
func (t *DeleteTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatDeleteData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatDeleteData formats delete-specific data
func (t *DeleteTool) formatDeleteData(data any) string {
	deleteResult, ok := data.(*domain.DeleteToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Path: %s\n", deleteResult.Path))
	output.WriteString(fmt.Sprintf("Total Files Deleted: %d\n", deleteResult.TotalFilesDeleted))
	output.WriteString(fmt.Sprintf("Total Directories Deleted: %d\n", deleteResult.TotalDirsDeleted))
	output.WriteString(fmt.Sprintf("Wildcard Expanded: %t\n", deleteResult.WildcardExpanded))

	if len(deleteResult.DeletedFiles) > 0 {
		output.WriteString("\nDeleted Files:\n")
		for _, file := range deleteResult.DeletedFiles {
			fileName := t.formatter.GetFileName(file)
			output.WriteString(fmt.Sprintf("  - %s\n", fileName))
		}
	}

	if len(deleteResult.DeletedDirs) > 0 {
		output.WriteString("\nDeleted Directories:\n")
		for _, dir := range deleteResult.DeletedDirs {
			dirName := t.formatter.GetFileName(dir)
			output.WriteString(fmt.Sprintf("  - %s/\n", dirName))
		}
	}

	if len(deleteResult.Errors) > 0 {
		output.WriteString("\nErrors:\n")
		for _, err := range deleteResult.Errors {
			output.WriteString(fmt.Sprintf("  - %s\n", err))
		}
	}

	return output.String()
}

// pluralize returns "s" for plural file count
func (t *DeleteTool) pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

// pluralizeDir returns "y" or "ies" for directory count
func (t *DeleteTool) pluralizeDir(count int) string {
	if count == 1 {
		return "y"
	}
	return "ies"
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *DeleteTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *DeleteTool) ShouldAlwaysExpand() bool {
	return false
}
