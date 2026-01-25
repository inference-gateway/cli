package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	"github.com/ledongthuc/pdf"
)

// Error constants for consistent error handling
const (
	ErrorNotAbsolutePath  = "NOT_ABSOLUTE_PATH"
	ErrorNotFound         = "NOT_FOUND"
	ErrorFileEmpty        = "FILE_EMPTY"
	ErrorPDFParseError    = "PDF_PARSE_ERROR"
	ErrorUnreadableBinary = "UNREADABLE_BINARY"
)

// Constants for defaults and limits
const (
	DefaultOffset     = 1
	DefaultLimit      = 2000
	MaxLineLength     = 2000
	EmptyFileReminder = "The file exists but is empty."
)

// ReadTool handles file reading operations with deterministic behavior
type ReadTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
}

// NewReadTool creates a new read tool
func NewReadTool(cfg *config.Config) *ReadTool {
	return &ReadTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Read.Enabled,
		formatter: domain.NewBaseFormatter("Read"),
	}
}

// Definition returns the tool definition for the LLM
func (t *ReadTool) Definition() sdk.ChatCompletionTool {
	description := `Reads a file from the local filesystem. You can access any file directly by using this tool.
Assume this tool is able to read all files on the machine. If the User provides a path to a file assume that path is valid. It is okay to read a file that does not exist; an error will be returned.

Usage:
- The file_path parameter can be either an absolute path or a relative path (relative paths will be resolved to absolute paths)
- By default, it reads up to 2000 lines starting from the beginning of the file
- You can optionally specify a line offset and limit (especially handy for long files), but it's recommended to read the whole file by not providing these parameters
- Any lines longer than 2000 characters will be truncated
- Results are returned using cat -n format, with line numbers starting at 1
- This tool can read PDF files (.pdf). PDFs are processed page by page, extracting both text and visual content for analysis.
- This tool cannot read image files. If the user wants to share an image, they should use the @ file reference syntax to attach it directly to their message.
- You have the capability to call multiple tools in a single response. It is always better to speculatively read multiple files as a batch that are potentially useful.
- If you read a file that exists but has empty contents you will receive a system reminder warning in place of file contents.`
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Read",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"file_path": map[string]any{
						"type":        "string",
						"description": "The path to the file to read (can be absolute or relative)",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "The number of lines to read. Only provide if the file is too large to read at once.",
						"minimum":     1,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "The line number to start reading from. Only provide if the file is too large to read at once",
						"minimum":     1,
					},
				},
				"required": []string{"file_path"},
			},
		},
	}
}

// Execute runs the read tool with given arguments
func (t *ReadTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("read tool is not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Read",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path parameter is required and must be a string",
		}, nil
	}

	offset := DefaultOffset
	if offsetFloat, ok := args["offset"].(float64); ok {
		offset = int(offsetFloat)
	}

	limit := DefaultLimit
	if limitFloat, ok := args["limit"].(float64); ok {
		limit = int(limitFloat)
	}

	// Check if file is an image - images cannot be read with this tool
	if t.isImageFile(filePath) {
		return &domain.ToolExecutionResult{
			ToolName:  "Read",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "Cannot read image files. Ask the user to share the image using the @ file reference syntax (e.g., @image.png) to attach it to their message.",
		}, nil
	}

	readResult, err := t.executeRead(filePath, offset, limit)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "Read",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	var toolData *domain.FileReadToolResult
	if readResult != nil {
		toolData = &domain.FileReadToolResult{
			FilePath:  readResult.FilePath,
			Content:   readResult.Content,
			Size:      readResult.Size,
			StartLine: readResult.StartLine,
			EndLine:   readResult.EndLine,
			Error:     readResult.Error,
		}
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Read",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	return result, nil
}

// Validate checks if the read tool arguments are valid
func (t *ReadTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("read tool is not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return fmt.Errorf("file_path parameter is required and must be a string")
	}

	if filePath == "" {
		return fmt.Errorf("file_path cannot be empty")
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for %s: %w", filePath, err)
	}

	if err := t.validatePathSecurity(absPath); err != nil {
		return err
	}

	return t.validateParameters(args)
}

// IsEnabled returns whether the read tool is enabled
func (t *ReadTool) IsEnabled() bool {
	return t.enabled
}

// FileReadResult represents the internal result of a file read operation
type FileReadResult struct {
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Error     string `json:"error,omitempty"`
}

// executeRead reads a file with offset and limit parameters
func (t *ReadTool) executeRead(filePath string, offset, limit int) (*FileReadResult, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for %s: %w", filePath, err)
	}

	result := &FileReadResult{
		FilePath:  absPath,
		StartLine: offset,
		EndLine:   offset + limit - 1,
	}

	if err := t.validatePathSecurity(absPath); err != nil {
		return nil, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s", ErrorNotFound)
		}
		return nil, fmt.Errorf("cannot access file %s: %w", absPath, err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("path %s is a directory, not a file", absPath)
	}

	if info.Size() == 0 {
		result.Content = EmptyFileReminder
		result.Size = int64(len(EmptyFileReminder))
		result.Error = ErrorFileEmpty
		return result, nil
	}

	ext := strings.ToLower(filepath.Ext(absPath))
	switch ext {
	case ".pdf":
		content, err := t.readPDF(absPath, offset, limit)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ErrorPDFParseError, err)
		}
		result.Content = content
		result.Size = int64(len(content))
		return result, nil
	default:
		content, err := t.readTextFile(absPath, offset, limit)
		if err != nil {
			return nil, err
		}
		result.Content = content
		result.Size = int64(len(content))
		return result, nil
	}
}

// readTextFile reads a text file with cat -n formatting
func (t *ReadTool) readTextFile(filePath string, offset, limit int) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	if !t.isTextFile(file) {
		return "", fmt.Errorf("%s", ErrorUnreadableBinary)
	}

	_, _ = file.Seek(0, 0)

	scanner := bufio.NewScanner(file)
	var lines []string
	lineNum := 1

	for scanner.Scan() {
		if lineNum >= offset && len(lines) < limit {
			line := scanner.Text()

			if len(line) > MaxLineLength {
				line = line[:MaxLineLength]
			}

			formattedLine := fmt.Sprintf("%6d\t%s", lineNum, line)
			lines = append(lines, formattedLine)
		}
		lineNum++

		if len(lines) >= limit {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading file %s: %w", filePath, err)
	}

	return strings.Join(lines, "\n"), nil
}

// readPDF reads a PDF file and extracts text with page headers
func (t *ReadTool) readPDF(filePath string, offset, limit int) (string, error) {
	file, reader, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	var lines []string
	lineNum := 1

	for pageNum := 1; pageNum <= reader.NumPage(); pageNum++ {
		page := reader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}

		if lineNum >= offset && len(lines) < limit {
			pageHeader := fmt.Sprintf("=== Page %d ===", pageNum)
			formattedLine := fmt.Sprintf("%6d\t%s", lineNum, pageHeader)
			lines = append(lines, formattedLine)
			lineNum++
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}

		pageLines := strings.Split(text, "\n")
		for _, line := range pageLines {
			if lineNum >= offset && len(lines) < limit {
				if len(line) > MaxLineLength {
					line = line[:MaxLineLength]
				}

				formattedLine := fmt.Sprintf("%6d\t%s", lineNum, line)
				lines = append(lines, formattedLine)
			}
			lineNum++

			if len(lines) >= limit {
				break
			}
		}

		if len(lines) >= limit {
			break
		}
	}

	return strings.Join(lines, "\n"), nil
}

// isTextFile checks if a file is likely to be text (not binary)
func (t *ReadTool) isTextFile(file *os.File) bool {
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil {
		return false
	}

	return utf8.Valid(buffer[:n])
}

// isImageFile checks if a file has an image extension
func (t *ReadTool) isImageFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	supportedExts := map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
		".webp": true,
	}
	return supportedExts[ext]
}

// validateParameters validates offset and limit parameters
func (t *ReadTool) validateParameters(args map[string]any) error {
	if err := t.validateParameter(args, "offset"); err != nil {
		return err
	}
	if err := t.validateParameter(args, "limit"); err != nil {
		return err
	}
	return nil
}

// validateParameter validates a single numeric parameter
func (t *ReadTool) validateParameter(args map[string]any, paramName string) error {
	value, exists := args[paramName]
	if !exists {
		return nil
	}

	floatValue, ok := value.(float64)
	if !ok {
		return fmt.Errorf("%s must be a number", paramName)
	}

	if floatValue < 1 {
		return fmt.Errorf("%s must be >= 1", paramName)
	}

	return nil
}

// validatePathSecurity checks if a path is allowed within the sandbox
func (t *ReadTool) validatePathSecurity(path string) error {
	return t.config.ValidatePathInSandbox(path)
}

// FormatResult formats tool execution results for different contexts
func (t *ReadTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *ReadTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	readResult, ok := result.Data.(*domain.FileReadToolResult)
	if !ok {
		if result.Success {
			return "File read completed successfully"
		}
		return "File read failed"
	}

	fileName := t.formatter.GetFileName(readResult.FilePath)
	if readResult.Content != "" {
		lineCount := strings.Count(readResult.Content, "\n") + 1
		return fmt.Sprintf("Read %d lines from %s", lineCount, fileName)
	}
	return fmt.Sprintf("Read %s", fileName)
}

// FormatForUI formats the result for UI display
func (t *ReadTool) FormatForUI(result *domain.ToolExecutionResult) string {
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
func (t *ReadTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatReadData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatReadData formats read-specific data
func (t *ReadTool) formatReadData(data any) string {
	readResult, ok := data.(*domain.FileReadToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("File: %s\n", readResult.FilePath))

	lineCount := 0
	if readResult.Content != "" {
		lineCount = strings.Count(readResult.Content, "\n") + 1
	}

	if readResult.StartLine > 0 {
		output.WriteString(fmt.Sprintf("Lines: %d", readResult.StartLine))
		if readResult.EndLine > 0 && readResult.EndLine != readResult.StartLine {
			output.WriteString(fmt.Sprintf("-%d", readResult.EndLine))
		}
		output.WriteString("\n")
	}

	output.WriteString(fmt.Sprintf("Lines: %d\n", lineCount))
	output.WriteString(fmt.Sprintf("Size: %d bytes\n", readResult.Size))

	if readResult.Error != "" {
		output.WriteString(fmt.Sprintf("Error: %s\n", readResult.Error))
	}
	if readResult.Content != "" {
		output.WriteString(fmt.Sprintf("Content:\n%s\n", readResult.Content))
	}
	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *ReadTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *ReadTool) ShouldAlwaysExpand() bool {
	return false
}
