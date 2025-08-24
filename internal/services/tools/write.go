package tools

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/domain/filewriter"
	filewriterservice "github.com/inference-gateway/cli/internal/services/filewriter"
)

const (
	ToolName      = "Write"
	DefaultFormat = "text"
	JSONFormat    = "json"
)

var (
	// Success styles
	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Bold(true)

	successIconStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("10"))

	// Error styles
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true)

	errorIconStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9"))

	// Path and file info styles
	pathStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true)

	fileInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))

	// Status styles
	createdStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Bold(true)

	updatedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true)

	appendedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true)

	// Metric styles
	metricStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("6"))
)

// WriteTool implements a refactored WriteTool with clean architecture
type WriteTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.CustomFormatter
	writer    filewriter.FileWriter
	chunks    filewriter.ChunkManager
	extractor *ParameterExtractor
}

// NewWriteTool creates a new write tool with clean architecture
func NewWriteTool(cfg *config.Config) *WriteTool {
	pathValidator := filewriterservice.NewPathValidator(cfg)
	backupManager := filewriterservice.NewBackupManager(".")
	fileWriter := filewriterservice.NewSafeFileWriter(pathValidator, backupManager)
	chunkManager := filewriterservice.NewStreamingChunkManager("./.infer/tmp", fileWriter)
	paramExtractor := NewParameterExtractor()

	return &WriteTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Write.Enabled,
		formatter: domain.NewCustomFormatter("Write", func(key string) bool {
			return key == "content"
		}),
		writer:    fileWriter,
		chunks:    chunkManager,
		extractor: paramExtractor,
	}
}

// Definition returns the tool definition for the LLM
func (t *WriteTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        ToolName,
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
				"overwrite": map[string]any{
					"type":        "boolean",
					"description": "Whether to overwrite existing files",
					"default":     true,
				},
				"backup": map[string]any{
					"type":        "boolean",
					"description": "Whether to create a backup of existing files before overwriting",
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
				"session_id": map[string]any{
					"type":        "string",
					"description": "Session ID for chunked operations to group related chunks together.",
				},
				"format": map[string]any{
					"type":        "string",
					"description": "Output format (text or json)",
					"enum":        []string{DefaultFormat, JSONFormat},
					"default":     DefaultFormat,
				},
			},
			"required": []string{"file_path", "content"},
		},
	}
}

// Execute runs the write tool with given arguments
func (t *WriteTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.enabled {
		return &domain.ToolExecutionResult{
			ToolName:  ToolName,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "write tool is not enabled",
		}, nil
	}

	params, err := t.extractor.ExtractWriteParams(args)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  ToolName,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("parameter extraction failed: %v", err),
		}, nil
	}

	format := t.extractFormat(args)

	var result *domain.ToolExecutionResult
	if params.IsChunked {
		result = t.executeChunked(ctx, params, args, start)
	} else if params.Append {
		result = t.executeAppend(ctx, params, args, start)
	} else {
		result = t.executeWrite(ctx, params, args, start)
	}

	if format == JSONFormat {
		result = t.formatAsJSON(result)
	}

	return result, nil
}

// Enabled returns whether the tool is enabled
func (t *WriteTool) Enabled() bool {
	return t.enabled
}

// IsEnabled returns whether the tool is enabled (interface compliance)
func (t *WriteTool) IsEnabled() bool {
	return t.enabled
}

// Validate checks if the write tool arguments are valid
func (t *WriteTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled || !t.config.Tools.Write.Enabled {
		return fmt.Errorf("write tool is not enabled")
	}

	_, err := t.extractor.ExtractWriteParams(args)
	return err
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
		return fileInfoStyle.Render("Write operation result unavailable")
	}

	if !result.Success {
		return errorStyle.Render("Write operation failed")
	}

	if result.Data == nil {
		return successStyle.Render("Write operation completed successfully")
	}

	// Format based on the actual result
	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		fileName := pathStyle.Render(t.formatter.GetFileName(writeResult.FilePath))
		bytes := metricStyle.Render(fmt.Sprintf("%d bytes", writeResult.BytesWritten))

		if writeResult.Created {
			return fmt.Sprintf("%s %s (%s)",
				createdStyle.Render("Created"), fileName, bytes)
		} else if writeResult.Appended {
			return fmt.Sprintf("%s %s (%s)",
				appendedStyle.Render("Appended to"), fileName, bytes)
		} else {
			return fmt.Sprintf("%s %s (%s)",
				updatedStyle.Render("Updated"), fileName, bytes)
		}
	}

	return successStyle.Render("Write operation completed")
}

// FormatForUI formats the result for UI display
func (t *WriteTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return errorStyle.Render("No result to display")
	}

	if !result.Success {
		return fmt.Sprintf("%s %s",
			errorIconStyle.Render("✗"),
			errorStyle.Render(fmt.Sprintf("Write failed: %s", result.Error)))
	}

	if result.Data == nil {
		return fmt.Sprintf("%s %s",
			successIconStyle.Render("✓"),
			successStyle.Render("Write completed successfully"))
	}

	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		var statusText string
		var statusStyle lipgloss.Style

		if writeResult.Created {
			statusText = "Created"
			statusStyle = createdStyle
		} else if writeResult.Appended {
			statusText = "Appended to"
			statusStyle = appendedStyle
		} else {
			statusText = "Updated"
			statusStyle = updatedStyle
		}

		icon := successIconStyle.Render("✓")
		status := statusStyle.Render(statusText)
		path := pathStyle.Render(writeResult.FilePath)

		metrics := fmt.Sprintf("(%s, %s)",
			metricStyle.Render(fmt.Sprintf("%d bytes", writeResult.BytesWritten)),
			metricStyle.Render(fmt.Sprintf("%d lines", writeResult.LinesWritten)))

		return fmt.Sprintf("%s %s %s %s", icon, status, path, fileInfoStyle.Render(metrics))
	}

	return fmt.Sprintf("%s %s",
		successIconStyle.Render("✓"),
		successStyle.Render("Write operation completed"))
}

// FormatForLLM formats the result for LLM consumption
func (t *WriteTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Write operation result unavailable"
	}

	if !result.Success {
		return fmt.Sprintf("Write operation failed: %s", result.Error)
	}

	if result.Data == nil {
		return "Write operation completed successfully"
	}

	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		action := "updated"
		if writeResult.Created {
			action = "created"
		} else if writeResult.Appended {
			action = "appended to"
		}

		return fmt.Sprintf("Successfully %s file %s (%d bytes written, %d lines)",
			action,
			writeResult.FilePath,
			writeResult.BytesWritten,
			writeResult.LinesWritten)
	}

	return "Write operation completed successfully"
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *WriteTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *WriteTool) ShouldAlwaysExpand() bool {
	return false
}

// executeWrite handles regular file write operations
func (t *WriteTool) executeWrite(ctx context.Context, params *WriteParams, args map[string]any, start time.Time) *domain.ToolExecutionResult {
	writeReq := t.extractor.ToWriteRequest(params)

	writeResult, err := t.writer.Write(ctx, writeReq)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  ToolName,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}
	}

	domainResult := &domain.FileWriteToolResult{
		FilePath:     writeResult.Path,
		BytesWritten: writeResult.BytesWritten,
		LinesWritten: countNewLines(params.Content),
		Created:      writeResult.Created,
		Overwritten:  !writeResult.Created && params.Overwrite,
		IsComplete:   true,
	}

	return &domain.ToolExecutionResult{
		ToolName:  ToolName,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      domainResult,
	}
}

// executeAppend handles append operations
func (t *WriteTool) executeAppend(ctx context.Context, params *WriteParams, args map[string]any, start time.Time) *domain.ToolExecutionResult {
	existingContent := ""
	if _, err := os.Stat(params.FilePath); err == nil {
		if data, err := os.ReadFile(params.FilePath); err == nil {
			existingContent = string(data)
		}
	}

	combinedContent := existingContent + params.Content

	writeReq := filewriter.WriteRequest{
		Path:      params.FilePath,
		Content:   combinedContent,
		Overwrite: true,
		Backup:    params.Backup,
	}

	writeResult, err := t.writer.Write(ctx, writeReq)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  ToolName,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}
	}

	domainResult := &domain.FileWriteToolResult{
		FilePath:     writeResult.Path,
		BytesWritten: int64(len(params.Content)),
		LinesWritten: countNewLines(params.Content),
		Created:      writeResult.Created,
		Appended:     true,
		IsComplete:   true,
	}

	return &domain.ToolExecutionResult{
		ToolName:  ToolName,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      domainResult,
	}
}

// executeChunked handles chunked write operations
func (t *WriteTool) executeChunked(ctx context.Context, params *WriteParams, args map[string]any, start time.Time) *domain.ToolExecutionResult {
	chunkReq := t.extractor.ToChunkWriteRequest(params)

	if err := t.chunks.WriteChunk(ctx, chunkReq); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "Write",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}
	}

	isComplete := chunkReq.IsLast

	var writeResult *filewriter.WriteResult
	if isComplete {
		var err error
		writeResult, err = t.chunks.FinalizeChunks(ctx, params.SessionID, params.FilePath)
		if err != nil {
			return &domain.ToolExecutionResult{
				ToolName:  "Write",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     err.Error(),
			}
		}
	}

	domainResult := &domain.FileWriteToolResult{
		BytesWritten: int64(len(params.Content)),
		LinesWritten: countNewLines(params.Content),
		IsComplete:   isComplete,
		ChunkIndex:   params.ChunkIndex,
		TotalChunks:  params.TotalChunks,
	}

	if writeResult != nil {
		domainResult.FilePath = writeResult.Path
		domainResult.Created = writeResult.Created
		domainResult.BytesWritten = writeResult.BytesWritten
	}

	return &domain.ToolExecutionResult{
		ToolName:  ToolName,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      domainResult,
	}
}

// extractFormat extracts the output format from arguments
func (t *WriteTool) extractFormat(args map[string]any) string {
	if format, ok := args["format"].(string); ok {
		return format
	}
	return DefaultFormat
}

// formatAsJSON converts result to JSON format
func (t *WriteTool) formatAsJSON(result *domain.ToolExecutionResult) *domain.ToolExecutionResult {
	// This would format the result as JSON - simplified for now
	return result
}

// countNewLines counts the number of lines in content (renamed to avoid conflict)
func countNewLines(content string) int {
	if content == "" {
		return 0
	}

	lines := 1
	for _, char := range content {
		if char == '\n' {
			lines++
		}
	}
	return lines
}
