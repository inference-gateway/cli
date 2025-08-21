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
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
}

// NewWriteTool creates a new write tool
func NewWriteTool(cfg *config.Config) *WriteTool {
	return &WriteTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Write.Enabled,
		formatter: domain.NewBaseFormatter("Write"),
	}
}

// Definition returns the tool definition for the LLM
func (t *WriteTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "Write",
		Description: "Write content to a file on the filesystem. Supports append mode and chunked writing for large files. Creates parent directories if needed.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "The path to the file to write",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The content to write to the file",
				},
				"append": map[string]any{
					"type":        "boolean",
					"description": "Whether to append to the file (true) or overwrite it (false)",
					"default":     false,
				},
				"chunk_index": map[string]any{
					"type":        "integer",
					"description": "The index of this chunk (0-based). Use for ordered chunks in large file writing.",
					"minimum":     0,
				},
				"total_chunks": map[string]any{
					"type":        "integer",
					"description": "The total number of chunks expected. Required when using chunk_index.",
					"minimum":     1,
				},
				"create_dirs": map[string]any{
					"type":        "boolean",
					"description": "Whether to create parent directories if they don't exist",
					"default":     true,
				},
				"overwrite": map[string]any{
					"type":        "boolean",
					"description": "Whether to overwrite existing files (ignored when append=true)",
					"default":     true,
				},
				"format": map[string]any{
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
func (t *WriteTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
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

	result := &domain.ToolExecutionResult{
		ToolName:  "Write",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      writeResult,
	}

	return result, nil
}

// Validate checks if the write tool arguments are valid
func (t *WriteTool) Validate(args map[string]any) error {
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

// executeWrite writes content to a file with support for append and chunked writing
func (t *WriteTool) executeWrite(filePath, content string, append bool, chunkIndex, totalChunks int, createDirs, overwrite bool) (*domain.FileWriteToolResult, error) {
	result := &domain.FileWriteToolResult{
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

	if totalChunks > 0 {
		return t.executeChunkedWrite(filePath, content, chunkIndex, totalChunks, result)
	}

	if append {
		return t.executeAppendWrite(filePath, content, result)
	}

	result.Overwritten = fileExists && overwrite
	if fileExists && !overwrite {
		return nil, fmt.Errorf("file %s already exists and overwrite is false", filePath)
	}

	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	result.BytesWritten = int64(len(content))
	result.LinesWritten = countLines(content)
	return result, nil
}

// createParentDirs creates parent directories if needed and allowed
func (t *WriteTool) createParentDirs(filePath string, createDirs bool, result *domain.FileWriteToolResult) error {
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

// validatePathSecurity checks if a path is allowed for writing within the sandbox
func (t *WriteTool) validatePathSecurity(path string) error {
	return t.config.ValidatePathInSandbox(path)
}

// executeAppendWrite handles writing in append mode
func (t *WriteTool) executeAppendWrite(filePath, content string, result *domain.FileWriteToolResult) (*domain.FileWriteToolResult, error) {
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
	result.LinesWritten = countLines(content)
	result.Appended = true
	return result, nil
}

// executeChunkedWrite handles writing in chunked mode using temporary files
func (t *WriteTool) executeChunkedWrite(filePath, content string, chunkIndex, totalChunks int, result *domain.FileWriteToolResult) (*domain.FileWriteToolResult, error) {
	tempDir := filepath.Join(filepath.Dir(filePath), ".infer_chunks_"+filepath.Base(filePath))
	chunkFile := filepath.Join(tempDir, fmt.Sprintf("chunk_%d", chunkIndex))

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory %s: %w", tempDir, err)
	}

	if err := os.WriteFile(chunkFile, []byte(content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write chunk file %s: %w", chunkFile, err)
	}

	result.BytesWritten = int64(len(content))
	result.LinesWritten = countLines(content)
	result.IsComplete = false

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
func getTotalChunks(args map[string]any, start time.Time) (int, *domain.ToolExecutionResult) {
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

// FormatResult formats tool execution results for different contexts
func (t *WriteTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *WriteTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	writeResult, ok := result.Data.(*domain.FileWriteToolResult)
	if !ok {
		if result.Success {
			return "File write completed successfully"
		}
		return "File write failed"
	}

	fileName := t.formatter.GetFileName(writeResult.FilePath)
	status := ""
	if writeResult.Created {
		status = "Created"
	} else if writeResult.Overwritten {
		status = "Overwritten"
	} else if writeResult.Appended {
		status = "Appended to"
	} else {
		status = "Updated"
	}

	if writeResult.TotalChunks > 0 {
		if writeResult.IsComplete {
			return fmt.Sprintf("%s %s (%d chunks combined)", status, fileName, writeResult.TotalChunks)
		}
		return fmt.Sprintf("Chunk %d/%d written to %s", writeResult.ChunkIndex+1, writeResult.TotalChunks, fileName)
	}

	return fmt.Sprintf("%s %s (%d lines)", status, fileName, writeResult.LinesWritten)
}

// FormatForUI formats the result for UI display
func (t *WriteTool) FormatForUI(result *domain.ToolExecutionResult) string {
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
func (t *WriteTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatWriteData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatWriteData formats write-specific data
func (t *WriteTool) formatWriteData(data any) string {
	writeResult, ok := data.(*domain.FileWriteToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("File: %s\n", writeResult.FilePath))
	output.WriteString(fmt.Sprintf("Bytes Written: %d\n", writeResult.BytesWritten))

	var operations []string
	if writeResult.Created {
		operations = append(operations, "created")
	}
	if writeResult.Overwritten {
		operations = append(operations, "overwritten")
	}
	if writeResult.Appended {
		operations = append(operations, "appended")
	}
	if writeResult.DirsCreated {
		operations = append(operations, "directories created")
	}

	if len(operations) > 0 {
		output.WriteString(fmt.Sprintf("Operations: %s\n", strings.Join(operations, ", ")))
	}

	if writeResult.TotalChunks > 0 {
		output.WriteString(fmt.Sprintf("Chunk: %d/%d\n", writeResult.ChunkIndex+1, writeResult.TotalChunks))
		output.WriteString(fmt.Sprintf("Complete: %t\n", writeResult.IsComplete))
	}

	if writeResult.Error != "" {
		output.WriteString(fmt.Sprintf("Error: %s\n", writeResult.Error))
	}

	return output.String()
}

// countLines counts the number of lines in the given content
func countLines(content string) int {
	if content == "" {
		return 0
	}
	lines := strings.Count(content, "\n")
	if !strings.HasSuffix(content, "\n") {
		lines++
	}
	return lines
}
