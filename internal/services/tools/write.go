package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	filewriter "github.com/inference-gateway/cli/internal/domain/filewriter"
	filewriterservice "github.com/inference-gateway/cli/internal/services/filewriter"
	components "github.com/inference-gateway/cli/internal/ui/components"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	sdk "github.com/inference-gateway/sdk"
)

const (
	ToolName      = "Write"
	DefaultFormat = "text"
	JSONFormat    = "json"
)

// WriteTool implements a refactored WriteTool with clean architecture
type WriteTool struct {
	config        *config.Config
	enabled       bool
	formatter     domain.CustomFormatter
	writer        filewriter.FileWriter
	chunks        filewriter.ChunkManager
	extractor     *ParameterExtractor
	styleProvider *styles.Provider
}

// NewWriteTool creates a new write tool with clean architecture
func NewWriteTool(cfg *config.Config) *WriteTool {
	pathValidator := filewriterservice.NewPathValidator(cfg)
	backupManager := filewriterservice.NewBackupManager(".")
	fileWriter := filewriterservice.NewSafeFileWriter(pathValidator, backupManager)
	chunkManager := filewriterservice.NewStreamingChunkManager("./.infer/tmp", fileWriter)
	paramExtractor := NewParameterExtractor()
	themeService := domain.NewThemeProvider()
	styleProvider := styles.NewProvider(themeService)

	return &WriteTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Write.Enabled,
		formatter: domain.NewCustomFormatter("Write", func(key string) bool {
			return key == "content"
		}),
		writer:        fileWriter,
		chunks:        chunkManager,
		extractor:     paramExtractor,
		styleProvider: styleProvider,
	}
}

// Definition returns the tool definition for the LLM
func (t *WriteTool) Definition() sdk.ChatCompletionTool {
	description := `Writes a file to the local filesystem.
Usage:
- This tool will overwrite the existing file if there is one at the provided path.
- If this is an existing file, you MUST use the Read tool first to read the file's contents. This tool will fail if you did not read the file first.
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.
- Only use emojis if the user explicitly requests it. Avoid writing emojis to files unless asked.`
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        ToolName,
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "The absolute path to the file to write (must be absolute, not relative)",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The content to write to the file",
					},
				},
				"required": []string{"file_path", "content"},
			},
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

	result := t.executeWrite(ctx, params, args, start)

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
		return t.styleProvider.RenderDimText("Write operation result unavailable")
	}

	if !result.Success {
		return t.styleProvider.RenderErrorText("Write operation failed")
	}

	if result.Data == nil {
		return t.styleProvider.RenderSuccessText("Write operation completed successfully")
	}

	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		fileName := t.styleProvider.RenderPathText(t.formatter.GetFileName(writeResult.FilePath))
		bytes := t.styleProvider.RenderMetricText(fmt.Sprintf("%d bytes", writeResult.BytesWritten))

		if writeResult.Created {
			return fmt.Sprintf("%s %s (%s)",
				t.styleProvider.RenderCreatedText("Created"), fileName, bytes)
		} else {
			return fmt.Sprintf("%s %s (%s)",
				t.styleProvider.RenderUpdatedText("Updated"), fileName, bytes)
		}
	}

	return t.styleProvider.RenderSuccessText("Write operation completed")
}

// FormatForUI formats the result for UI display
func (t *WriteTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	statusIcon := t.formatter.FormatStatusIcon(result.Success)

	var output strings.Builder
	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	output.WriteString(fmt.Sprintf("%s\n", toolCall))

	if !result.Success {
		output.WriteString(fmt.Sprintf("└─ %s Write failed: %s", statusIcon, result.Error))
		return output.String()
	}

	if writeResult, ok := result.Data.(*domain.FileWriteToolResult); ok {
		action := "Updated"
		if writeResult.Created {
			action = "Created"
		}
		output.WriteString(fmt.Sprintf("└─ %s %s file (%d bytes, %d lines)",
			statusIcon, action, writeResult.BytesWritten, writeResult.LinesWritten))
		return output.String()
	}

	output.WriteString(fmt.Sprintf("└─ %s Write completed", statusIcon))
	return output.String()
}

// FormatForLLM formats the result for LLM consumption with expanded tree structure
func (t *WriteTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Write operation result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))
	output.WriteString(t.formatWriteResultData(result))

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatWriteResultData formats the write result data section
func (t *WriteTool) formatWriteResultData(result *domain.ToolExecutionResult) string {
	if result.Data == nil {
		return ""
	}

	writeResult, ok := result.Data.(*domain.FileWriteToolResult)
	if !ok {
		return ""
	}

	action := "Updated"
	if writeResult.Created {
		action = "Created"
	}

	connector := "└─"
	if len(result.Metadata) > 0 {
		connector = "├─"
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s Result:\n", connector))
	output.WriteString(fmt.Sprintf("   %s file: %s\n", action, writeResult.FilePath))
	output.WriteString(fmt.Sprintf("   Bytes written: %d\n", writeResult.BytesWritten))
	output.WriteString(fmt.Sprintf("   Lines: %d\n", writeResult.LinesWritten))

	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *WriteTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *WriteTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatArgumentsForApproval formats arguments for approval display with content preview
func (t *WriteTool) FormatArgumentsForApproval(args map[string]any) string {
	themeService := domain.NewThemeProvider()
	styleProvider := styles.NewProvider(themeService)
	diffRenderer := components.NewToolDiffRenderer(styleProvider)
	return diffRenderer.RenderWriteToolArguments(args)
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
		Overwritten:  !writeResult.Created,
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

// extractFormat extracts the output format from arguments
func (t *WriteTool) extractFormat(args map[string]any) string {
	if format, ok := args["format"].(string); ok {
		return format
	}
	return DefaultFormat
}

// formatAsJSON converts result to JSON format
func (t *WriteTool) formatAsJSON(result *domain.ToolExecutionResult) *domain.ToolExecutionResult {
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
