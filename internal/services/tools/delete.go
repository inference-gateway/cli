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
	config  *config.Config
	enabled bool
}

// NewDeleteTool creates a new delete tool
func NewDeleteTool(cfg *config.Config) *DeleteTool {
	return &DeleteTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Delete.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *DeleteTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "Delete",
		Description: "Delete files or directories from the filesystem. Supports wildcard patterns for batch operations. Restricted to current working directory for security.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file or directory to delete. Supports wildcard patterns like '*.txt' or 'temp/*' when wildcards are enabled.",
				},
				"recursive": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to delete directories recursively",
					"default":     false,
				},
				"force": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to force deletion (ignore non-existent files)",
					"default":     false,
				},
				"format": map[string]interface{}{
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
func (t *DeleteTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
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
func (t *DeleteTool) Validate(args map[string]interface{}) error {
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
		if !t.config.Tools.Delete.AllowWildcards {
			return nil, fmt.Errorf("wildcard patterns are not enabled in the configuration")
		}
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

// validatePathSecurity checks if a path is allowed for deletion
func (t *DeleteTool) validatePathSecurity(path string) error {
	if t.config.Tools.Delete.RestrictToWorkDir {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current working directory: %w", err)
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute path for %s: %w", path, err)
		}

		if !strings.HasPrefix(absPath, wd) {
			return fmt.Errorf("path '%s' is outside the current working directory", path)
		}
	}

	for _, excludePath := range t.config.Tools.ExcludePaths {
		if strings.HasPrefix(path, excludePath) {
			return fmt.Errorf("access to path '%s' is excluded for security", path)
		}

		if strings.Contains(excludePath, "*") && matchesPattern(path, excludePath) {
			return fmt.Errorf("access to path '%s' is excluded for security", path)
		}
	}

	for _, protectedPath := range t.config.Tools.Delete.ProtectedPaths {
		if strings.HasPrefix(path, protectedPath) {
			return fmt.Errorf("path '%s' is protected from deletion", path)
		}

		if strings.Contains(protectedPath, "*") && matchesPattern(path, protectedPath) {
			return fmt.Errorf("path '%s' is protected from deletion", path)
		}
	}

	return nil
}
