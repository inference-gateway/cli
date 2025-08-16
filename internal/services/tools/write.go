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
		Description: "Write content to a file on the filesystem. Supports append mode and chunked writing for large files. Creates parent directories if needed.",
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
				"append": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to append to the file (true) or overwrite it (false)",
					"default":     false,
				},
				"chunk_index": map[string]interface{}{
					"type":        "integer",
					"description": "The index of this chunk (0-based). Use for ordered chunks in large file writing.",
					"minimum":     0,
				},
				"total_chunks": map[string]interface{}{
					"type":        "integer",
					"description": "The total number of chunks expected. Required when using chunk_index.",
					"minimum":     1,
				},
				"create_dirs": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to create parent directories if they don't exist",
					"default":     true,
				},
				"overwrite": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to overwrite existing files (ignored when append=true)",
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

	append := false
	if appendVal, exists := args["append"]; exists {
		if val, ok := appendVal.(bool); ok {
			append = val
		}
	}

	var chunkIndex, totalChunks int
	if chunkIndexVal, exists := args["chunk_index"]; exists {
		if val, ok := chunkIndexVal.(float64); ok {
			chunkIndex = int(val)
			var err *domain.ToolExecutionResult
			totalChunks, err = getTotalChunks(args, start)
			if err != nil {
				return err, nil
			}
		}
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

	writeResult, err := t.executeWrite(filePath, content, append, chunkIndex, totalChunks, createDirs, overwrite)
	if err != nil {
		return nil, err
	}

	var toolData *domain.FileWriteToolResult
	if writeResult != nil {
		toolData = &domain.FileWriteToolResult{
			FilePath:     writeResult.FilePath,
			BytesWritten: writeResult.BytesWritten,
			Created:      writeResult.Created,
			Overwritten:  writeResult.Overwritten,
			DirsCreated:  writeResult.DirsCreated,
			Appended:     writeResult.Appended,
			ChunkIndex:   writeResult.ChunkIndex,
			TotalChunks:  writeResult.TotalChunks,
			IsComplete:   writeResult.IsComplete,
			Error:        writeResult.Error,
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

	if append, exists := args["append"]; exists {
		if _, ok := append.(bool); !ok {
			return fmt.Errorf("append parameter must be a boolean")
		}
	}

	if chunkIndex, exists := args["chunk_index"]; exists {
		if _, ok := chunkIndex.(float64); !ok {
			return fmt.Errorf("chunk_index parameter must be a number")
		}
		if _, exists := args["total_chunks"]; !exists {
			return fmt.Errorf("total_chunks parameter is required when using chunk_index")
		}
	}

	if totalChunks, exists := args["total_chunks"]; exists {
		if _, ok := totalChunks.(float64); !ok {
			return fmt.Errorf("total_chunks parameter must be a number")
		}
		if _, exists := args["chunk_index"]; !exists {
			return fmt.Errorf("chunk_index parameter is required when using total_chunks")
		}
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
	FilePath     string `json:"file_path"`
	BytesWritten int64  `json:"bytes_written"`
	Created      bool   `json:"created"`
	Overwritten  bool   `json:"overwritten"`
	DirsCreated  bool   `json:"dirs_created"`
	Appended     bool   `json:"appended"`
	ChunkIndex   int    `json:"chunk_index,omitempty"`
	TotalChunks  int    `json:"total_chunks,omitempty"`
	IsComplete   bool   `json:"is_complete"`
	Error        string `json:"error,omitempty"`
}

// executeWrite writes content to a file with support for append and chunked writing
func (t *WriteTool) executeWrite(filePath, content string, append bool, chunkIndex, totalChunks int, createDirs, overwrite bool) (*FileWriteResult, error) {
	result := &FileWriteResult{
		FilePath:    filePath,
		Appended:    append,
		ChunkIndex:  chunkIndex,
		TotalChunks: totalChunks,
		IsComplete:  true,
	}

	_, err := os.Stat(filePath)
	fileExists := err == nil
	result.Created = !fileExists

	if err := t.createParentDirs(filePath, createDirs, result); err != nil {
		return nil, err
	}

	// Handle chunked writing
	if totalChunks > 0 {
		return t.executeChunkedWrite(filePath, content, chunkIndex, totalChunks, result)
	}

	// Handle simple write or append
	if append {
		return t.executeAppendWrite(filePath, content, result)
	}

	// Handle overwrite mode
	result.Overwritten = fileExists && overwrite
	if fileExists && !overwrite {
		return nil, fmt.Errorf("file %s already exists and overwrite is false", filePath)
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	result.BytesWritten = int64(len(content))
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

// executeAppendWrite handles writing in append mode
func (t *WriteTool) executeAppendWrite(filePath, content string, result *FileWriteResult) (*FileWriteResult, error) {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s for appending: %w", filePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	bytesWritten, err := file.WriteString(content)
	if err != nil {
		return nil, fmt.Errorf("failed to append to file %s: %w", filePath, err)
	}

	result.BytesWritten = int64(bytesWritten)
	result.Appended = true
	return result, nil
}

// executeChunkedWrite handles writing in chunked mode using temporary files
func (t *WriteTool) executeChunkedWrite(filePath, content string, chunkIndex, totalChunks int, result *FileWriteResult) (*FileWriteResult, error) {
	tempDir := filepath.Join(filepath.Dir(filePath), ".infer_chunks_"+filepath.Base(filePath))
	chunkFile := filepath.Join(tempDir, fmt.Sprintf("chunk_%d", chunkIndex))

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
	}

	if err := os.WriteFile(chunkFile, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write chunk file %s: %w", chunkFile, err)
	}

	result.BytesWritten = int64(len(content))
	result.IsComplete = false

	// If this is the last chunk, combine all chunks into the final file
	if chunkIndex == totalChunks-1 {
		if err := t.combineChunks(filePath, tempDir, totalChunks); err != nil {
			return nil, fmt.Errorf("failed to combine chunks: %w", err)
		}

		if err := os.RemoveAll(tempDir); err != nil {
			return nil, fmt.Errorf("failed to clean up temp directory %s: %w", tempDir, err)
		}
		result.IsComplete = true
	}

	return result, nil
}

// combineChunks combines all chunk files into the final file
func (t *WriteTool) combineChunks(filePath, tempDir string, totalChunks int) error {
	finalFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create final file %s: %w", filePath, err)
	}
	defer func() {
		_ = finalFile.Close()
	}()

	for i := 0; i < totalChunks; i++ {
		chunkFile := filepath.Join(tempDir, fmt.Sprintf("chunk_%d", i))
		chunkContent, err := os.ReadFile(chunkFile)
		if err != nil {
			return fmt.Errorf("failed to read chunk file %s: %w", chunkFile, err)
		}

		if _, err := finalFile.Write(chunkContent); err != nil {
			return fmt.Errorf("failed to write chunk %d to final file: %w", i, err)
		}
	}

	return nil
}

// getTotalChunks extracts and validates total_chunks parameter
func getTotalChunks(args map[string]interface{}, start time.Time) (int, *domain.ToolExecutionResult) {
	totalChunksVal, exists := args["total_chunks"]
	if !exists {
		return 0, &domain.ToolExecutionResult{
			ToolName:  "Write",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "total_chunks parameter is required when using chunk_index",
		}
	}

	val, ok := totalChunksVal.(float64)
	if !ok {
		return 0, &domain.ToolExecutionResult{
			ToolName:  "Write",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "total_chunks parameter is required when using chunk_index",
		}
	}

	return int(val), nil
}
