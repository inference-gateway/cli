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

// WriteChunkTool handles chunked file writing operations to the filesystem
type WriteChunkTool struct {
	config  *config.Config
	enabled bool
}

// NewWriteChunkTool creates a new write chunk tool
func NewWriteChunkTool(cfg *config.Config) *WriteChunkTool {
	return &WriteChunkTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Write.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *WriteChunkTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "WriteChunk",
		Description: "Write content to a file in chunks. Useful for large files that need to be written in parts. Supports both append mode and indexed chunks.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{
					"type":        "string",
					"description": "The path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The content chunk to write to the file",
				},
				"chunk_index": map[string]interface{}{
					"type":        "integer",
					"description": "The index of this chunk (0-based). Use for ordered chunks.",
					"minimum":     0,
				},
				"total_chunks": map[string]interface{}{
					"type":        "integer",
					"description": "The total number of chunks expected. Required when using chunk_index.",
					"minimum":     1,
				},
				"append_mode": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to append to the file (true) or use indexed chunks (false)",
					"default":     true,
				},
				"create_dirs": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to create parent directories if they don't exist",
					"default":     true,
				},
				"overwrite": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to overwrite existing files on first chunk",
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

// Execute runs the write chunk tool with given arguments
func (t *WriteChunkTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("write chunk tool is not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteChunk",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path parameter is required and must be a string",
		}, nil
	}

	content, ok := args["content"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteChunk",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "content parameter is required and must be a string",
		}, nil
	}

	appendMode := true
	if appendModeVal, exists := args["append_mode"]; exists {
		if val, ok := appendModeVal.(bool); ok {
			appendMode = val
		}
	}

	var chunkIndex, totalChunks int
	if !appendMode {
		var err error
		chunkIndex, totalChunks, err = t.parseIndexedModeArgs(args)
		if err != nil {
			return &domain.ToolExecutionResult{
				ToolName:  "WriteChunk",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     err.Error(),
			}, nil
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

	writeResult, err := t.executeWriteChunk(filePath, content, appendMode, chunkIndex, totalChunks, createDirs, overwrite)
	if err != nil {
		return nil, err
	}

	var toolData *domain.FileWriteChunkToolResult
	if writeResult != nil {
		toolData = &domain.FileWriteChunkToolResult{
			FilePath:     writeResult.FilePath,
			BytesWriten:  writeResult.BytesWriten,
			ChunkIndex:   writeResult.ChunkIndex,
			TotalChunks:  writeResult.TotalChunks,
			AppendMode:   writeResult.AppendMode,
			Created:      writeResult.Created,
			Overwritten:  writeResult.Overwritten,
			DirsCreated:  writeResult.DirsCreated,
			IsComplete:   writeResult.IsComplete,
			Error:        writeResult.Error,
		}
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "WriteChunk",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	return result, nil
}

// Validate checks if the write chunk tool arguments are valid
func (t *WriteChunkTool) Validate(args map[string]interface{}) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("write chunk tool is not enabled")
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

	appendMode := true
	if appendModeVal, exists := args["append_mode"]; exists {
		if val, ok := appendModeVal.(bool); !ok {
			return fmt.Errorf("append_mode parameter must be a boolean")
		} else {
			appendMode = val
		}
	}

	if !appendMode {
		chunkIndexVal, hasIndex := args["chunk_index"]
		totalChunksVal, hasTotal := args["total_chunks"]

		if !hasIndex || !hasTotal {
			return fmt.Errorf("chunk_index and total_chunks are required when append_mode is false")
		}

		if _, ok := chunkIndexVal.(float64); !ok {
			return fmt.Errorf("chunk_index must be a number")
		}

		if _, ok := totalChunksVal.(float64); !ok {
			return fmt.Errorf("total_chunks must be a number")
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

// IsEnabled returns whether the write chunk tool is enabled
func (t *WriteChunkTool) IsEnabled() bool {
	return t.enabled
}

// FileWriteChunkResult represents the internal result of a chunked file write operation
type FileWriteChunkResult struct {
	FilePath     string `json:"file_path"`
	BytesWriten  int64  `json:"bytes_written"`
	ChunkIndex   int    `json:"chunk_index,omitempty"`
	TotalChunks  int    `json:"total_chunks,omitempty"`
	AppendMode   bool   `json:"append_mode"`
	Created      bool   `json:"created"`
	Overwritten  bool   `json:"overwritten"`
	DirsCreated  bool   `json:"dirs_created"`
	IsComplete   bool   `json:"is_complete"`
	Error        string `json:"error,omitempty"`
}

// executeWriteChunk writes content to a file in chunks
func (t *WriteChunkTool) executeWriteChunk(filePath, content string, appendMode bool, chunkIndex, totalChunks int, createDirs, overwrite bool) (*FileWriteChunkResult, error) {
	result := &FileWriteChunkResult{
		FilePath:    filePath,
		AppendMode:  appendMode,
		ChunkIndex:  chunkIndex,
		TotalChunks: totalChunks,
	}

	// Check if file exists
	_, err := os.Stat(filePath)
	fileExists := err == nil


	// Handle file existence and overwrite logic
	result.Overwritten = fileExists && overwrite
	result.Created = !fileExists

	// Only prevent overwrite in non-append mode (indexed mode)
	if !appendMode && fileExists && !overwrite {
		return nil, fmt.Errorf("file %s already exists and overwrite is false", filePath)
	}

	// Create parent directories if needed
	if err := t.createParentDirs(filePath, createDirs, result); err != nil {
		return nil, err
	}

	// Write the chunk
	var writeErr error
	if appendMode {
		// Simple approach: in append mode, only truncate if the file exists, overwrite is true,
		// and this is the first chunk (for append mode, every call is considered "first" since there's no indexing)
		shouldTruncate := fileExists && overwrite
		writeErr = t.writeAppendMode(filePath, content, shouldTruncate, result)
	} else {
		writeErr = t.writeIndexedMode(filePath, content, chunkIndex, totalChunks, result)
	}

	return result, writeErr
}

// parseIndexedModeArgs parses and validates chunk_index and total_chunks for indexed mode
func (t *WriteChunkTool) parseIndexedModeArgs(args map[string]interface{}) (int, int, error) {
	chunkIndexVal, hasIndex := args["chunk_index"]
	totalChunksVal, hasTotal := args["total_chunks"]

	if !hasIndex || !hasTotal {
		return 0, 0, fmt.Errorf("chunk_index and total_chunks are required when append_mode is false")
	}

	idx, ok := chunkIndexVal.(float64)
	if !ok {
		return 0, 0, fmt.Errorf("chunk_index must be a number")
	}
	chunkIndex := int(idx)

	total, ok := totalChunksVal.(float64)
	if !ok {
		return 0, 0, fmt.Errorf("total_chunks must be a number")
	}
	totalChunks := int(total)

	if chunkIndex < 0 || chunkIndex >= totalChunks {
		return 0, 0, fmt.Errorf("chunk_index (%d) must be between 0 and %d", chunkIndex, totalChunks-1)
	}

	return chunkIndex, totalChunks, nil
}

// writeAppendMode handles writing in append mode
func (t *WriteChunkTool) writeAppendMode(filePath, content string, shouldTruncate bool, result *FileWriteChunkResult) error {
	var flags int
	if shouldTruncate {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	} else {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}

	file, err := os.OpenFile(filePath, flags, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s for writing: %w", filePath, err)
	}

	var writeErr error
	defer func() {
		if closeErr := file.Close(); closeErr != nil && writeErr == nil {
			writeErr = closeErr
		}
	}()

	bytesWritten, err := file.WriteString(content)
	if err != nil {
		writeErr = fmt.Errorf("failed to write chunk to file %s: %w", filePath, err)
		return writeErr
	}

	result.BytesWriten = int64(bytesWritten)
	result.IsComplete = true
	return writeErr
}

// writeIndexedMode handles writing in indexed mode using temporary files
func (t *WriteChunkTool) writeIndexedMode(filePath, content string, chunkIndex, totalChunks int, result *FileWriteChunkResult) error {
	tempDir := filepath.Join(filepath.Dir(filePath), ".infer_chunks_"+filepath.Base(filePath))
	chunkFile := filepath.Join(tempDir, fmt.Sprintf("chunk_%d", chunkIndex))

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
	}

	if err := os.WriteFile(chunkFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write chunk file %s: %w", chunkFile, err)
	}

	result.BytesWriten = int64(len(content))

	if chunkIndex == totalChunks-1 {
		if err := t.combineChunks(filePath, tempDir, totalChunks); err != nil {
			return fmt.Errorf("failed to combine chunks: %w", err)
		}

		if err := os.RemoveAll(tempDir); err != nil {
			return fmt.Errorf("failed to clean up temp directory %s: %w", tempDir, err)
		}
		result.IsComplete = true
	}

	return nil
}

// combineChunks combines all chunk files into the final file
func (t *WriteChunkTool) combineChunks(filePath, tempDir string, totalChunks int) error {
	finalFile, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create final file %s: %w", filePath, err)
	}
	defer func() {
		if closeErr := finalFile.Close(); closeErr != nil {
			err = closeErr
		}
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

// createParentDirs creates parent directories if needed and allowed
func (t *WriteChunkTool) createParentDirs(filePath string, createDirs bool, result *FileWriteChunkResult) error {
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

// validatePathSecurity checks if a path is allowed for writing
func (t *WriteChunkTool) validatePathSecurity(path string) error {
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

