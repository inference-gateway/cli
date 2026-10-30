package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	filewriter "github.com/inference-gateway/cli/internal/tools/filewriter"
)

const (
	DefaultFormat = "text"
	JSONFormat    = "json"
)

// WriteTool implements a refactored WriteTool with clean architecture
type WriteTool struct {
	config    *config.Config
	enabled   bool
	formatter BaseFormatter
	writer    *filewriter.SafeFileWriter
	chunks    *filewriter.StreamingChunkBuffer
}

// NewWriteTool creates a new write tool with clean architecture
func NewWriteTool(cfg *config.Config) *WriteTool {
	pathValidator := filewriter.NewPathValidator(cfg)
	runtimeDir := config.ProjectRuntimeDir()
	backupManager := filewriter.NewBackup(filepath.Join(runtimeDir, "backups"))
	fileWriter := filewriter.NewSafeFileWriter(pathValidator, backupManager)
	chunkManager := filewriter.NewStreamingChunkBuffer(config.ProjectTmpDir(), fileWriter)

	return &WriteTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Write.Enabled,
		formatter: NewCustomFormatter(ToolWrite, func(key string) bool {
			return key == "content"
		}),
		writer: fileWriter,
		chunks: chunkManager,
	}
}

// Manifest returns the tool's manifest with its configured require_approval.
func (t *WriteTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolWrite).WithRequireApproval(t.config.Tools.Write.RequireApproval)
}

// Definition returns the tool definition for the LLM
func (t *WriteTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

// Execute runs the write tool with given arguments
func (t *WriteTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.enabled {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolWrite,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "write tool is not enabled",
		}, nil
	}

	writeReq, err := writeRequest(args)
	if err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolWrite,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("parameter extraction failed: %v", err),
		}, nil
	}

	result := t.executeWrite(ctx, writeReq, args, start)

	format := t.extractFormat(args)
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

	_, err := writeRequest(args)
	return err
}

// FormatResult formats tool execution results for different contexts
func (t *WriteTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	return formatResult(result, formatType, t.FormatForUI, t.FormatForLLM, t.FormatPreview)
}

// FormatPreview returns a short preview of the result for UI display
func (t *WriteTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Write operation result unavailable"
	}

	if !result.Success {
		return "Write operation failed"
	}

	if result.Data == nil {
		return "Write operation completed successfully"
	}

	if writeResult, ok := result.Data.(*agentdomain.FileWriteToolResult); ok {
		fileName := t.formatter.GetFileName(writeResult.FilePath)
		action := "Updated"
		if writeResult.Created {
			action = "Created"
		}
		return fmt.Sprintf("%s %s (%d bytes)", action, fileName, writeResult.BytesWritten)
	}

	return "Write operation completed"
}

// FormatForUI formats the result for UI display
func (t *WriteTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	if !result.Success {
		return t.formatter.FormatCollapsed(result, "Write failed: "+result.Error)
	}
	if writeResult, ok := result.Data.(*agentdomain.FileWriteToolResult); ok {
		action := "Updated"
		if writeResult.Created {
			action = "Created"
		}
		return t.formatter.FormatCollapsed(result, fmt.Sprintf("%s file (%d bytes, %d lines)", action, writeResult.BytesWritten, writeResult.LinesWritten))
	}
	return t.formatter.FormatCollapsed(result, "Write completed")
}

// FormatForLLM formats the result for LLM consumption with expanded tree structure
func (t *WriteTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Write operation result unavailable"
	}

	return t.formatter.FormatExpanded(result, t.formatWriteResultData(result))
}

// formatWriteResultData formats the write result data section
func (t *WriteTool) formatWriteResultData(result *agentdomain.ToolExecutionResult) string {
	if result.Data == nil {
		return ""
	}

	writeResult, ok := result.Data.(*agentdomain.FileWriteToolResult)
	if !ok {
		return ""
	}

	action := "Updated"
	if writeResult.Created {
		action = "Created"
	}

	var output strings.Builder
	fmt.Fprintf(&output, "%s file: %s\n", action, writeResult.FilePath)
	fmt.Fprintf(&output, "Bytes written: %d\n", writeResult.BytesWritten)
	fmt.Fprintf(&output, "Lines: %d\n", writeResult.LinesWritten)

	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *WriteTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

// executeWrite handles regular file write operations
func (t *WriteTool) executeWrite(ctx context.Context, writeReq filewriter.WriteRequest, args map[string]any, start time.Time) *agentdomain.ToolExecutionResult {
	writeResult, err := t.writer.Write(ctx, writeReq)
	if err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolWrite,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}
	}

	domainResult := &agentdomain.FileWriteToolResult{
		FilePath:     writeResult.Path,
		BytesWritten: writeResult.BytesWritten,
		LinesWritten: countNewLines(writeReq.Content),
		Created:      writeResult.Created,
		Overwritten:  !writeResult.Created,
		IsComplete:   true,
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolWrite,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      domainResult,
	}
}

// writeRequest reads the file_path and content arguments into a request that
// overwrites the file.
func writeRequest(args map[string]any) (filewriter.WriteRequest, error) {
	filePath, err := requiredString(args, "file_path")
	if err != nil {
		return filewriter.WriteRequest{}, err
	}
	content, err := requiredString(args, "content")
	if err != nil {
		return filewriter.WriteRequest{}, err
	}
	return filewriter.WriteRequest{Path: filePath, Content: content, Overwrite: true}, nil
}

// requiredString reads an argument that must be a non-empty string.
func requiredString(args map[string]any, key string) (string, error) {
	value, exists := args[key]
	if !exists {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	str, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s must be a string, got %T", key, value)
	}
	if str == "" {
		return "", fmt.Errorf("parameter %s cannot be empty", key)
	}
	return str, nil
}

// extractFormat extracts the output format from arguments
func (t *WriteTool) extractFormat(args map[string]any) string {
	if format, ok := args["format"].(string); ok {
		return format
	}
	return DefaultFormat
}

// formatAsJSON converts result to JSON format
func (t *WriteTool) formatAsJSON(result *agentdomain.ToolExecutionResult) *agentdomain.ToolExecutionResult {
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
