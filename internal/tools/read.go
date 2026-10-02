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

	pdf "github.com/ledongthuc/pdf"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sandbox "github.com/inference-gateway/cli/internal/sandbox"
)

// Error constants for consistent error handling
const (
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
	formatter BaseFormatter
}

// NewReadTool creates a new read tool
func NewReadTool(cfg *config.Config) *ReadTool {
	return &ReadTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Read.Enabled,
		formatter: NewBaseFormatter(ToolRead),
	}
}

// Manifest returns the tool's manifest with its configured require_approval.
func (t *ReadTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolRead).WithRequireApproval(t.config.Tools.Read.RequireApproval)
}

// Definition returns the tool definition for the LLM
func (t *ReadTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

// Execute runs the read tool with given arguments
func (t *ReadTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("read tool is not enabled")
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolRead,
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

	if t.isImageFile(filePath) {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolRead,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "Cannot read image files. Pass the file path directly to image tools instead (e.g. ImageEdit or ImageVariation take a local image path); there is no need to read the bytes first.",
		}, nil
	}

	readResult, err := t.executeRead(filePath, offset, limit)
	if err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolRead,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	var toolData *agentdomain.FileReadToolResult
	if readResult != nil {
		toolData = &agentdomain.FileReadToolResult{
			FilePath:  readResult.FilePath,
			Content:   readResult.Content,
			Size:      readResult.Size,
			StartLine: readResult.StartLine,
			EndLine:   readResult.EndLine,
			Error:     readResult.Error,
		}
	}

	result := &agentdomain.ToolExecutionResult{
		ToolName:  ToolRead,
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
	}

	if err := t.validatePathSecurity(absPath); err != nil {
		return nil, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s: %s%s", ErrorNotFound, absPath, t.notFoundHint(absPath))
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
		content, actualEndLine, err := t.readPDF(absPath, offset, limit)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ErrorPDFParseError, err)
		}
		result.Content = content
		result.Size = int64(len(content))
		result.EndLine = actualEndLine
		return result, nil
	default:
		content, actualEndLine, err := t.readTextFile(absPath, offset, limit)
		if err != nil {
			return nil, err
		}
		result.Content = content
		result.Size = int64(len(content))
		result.EndLine = actualEndLine
		return result, nil
	}
}

// notFoundHint lists candidate paths for a missing file so the model can
// correct a typo or a wrong directory instead of retrying the same path.
func (t *ReadTool) notFoundHint(absPath string) string {
	candidates := t.nearbyPaths(absPath)
	if len(candidates) == 0 {
		return ""
	}
	return "\n\nDid you mean one of:\n  " + strings.Join(candidates, "\n  ")
}

const maxNearbyPaths = 5

// nearbyPaths ranks the siblings of the nearest existing ancestor by name
// similarity and, when none carries the missing base name, searches the
// sandbox root for files with that base name.
func (t *ReadTool) nearbyPaths(absPath string) []string {
	dir, missing := nearestExistingDir(absPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	want := strings.ToLower(strings.TrimSuffix(missing, filepath.Ext(missing)))
	base := strings.ToLower(filepath.Base(absPath))
	var similar, others []string
	sameName := false
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if name == base {
			sameName = true
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if want != "" && (strings.Contains(stem, want) || strings.Contains(want, stem)) {
			similar = append(similar, filepath.Join(dir, e.Name()))
		} else {
			others = append(others, filepath.Join(dir, e.Name()))
		}
	}

	candidates := similar
	if !sameName {
		candidates = append(candidates, t.sameBaseNameInSandbox(absPath, dir)...)
	}
	candidates = append(candidates, others...)
	return candidates[:min(maxNearbyPaths, len(candidates))]
}

// nearestExistingDir walks up from path until a directory exists and returns
// it with the first path component that was missing below it.
func nearestExistingDir(path string) (string, string) {
	missing := filepath.Base(path)
	dir := filepath.Dir(path)
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, missing
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir, missing
		}
		missing = filepath.Base(dir)
		dir = parent
	}
}

// sameBaseNameInSandbox finds files sharing the missing file's base name under
// the sandbox directory containing it, falling back to the nearest existing dir.
func (t *ReadTool) sameBaseNameInSandbox(absPath, fallback string) []string {
	root := fallback
	for _, sandboxDir := range t.config.Tools.Sandbox.Directories {
		if abs, err := filepath.Abs(sandboxDir); err == nil && isUnderDir(absPath, abs) {
			root = abs
			break
		}
	}

	base := strings.ToLower(filepath.Base(absPath))
	var hits []string
	visited := 0
	// ponytail: walk budget 20k entries, index the tree if repos outgrow it
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > 20000 || len(hits) >= maxNearbyPaths {
			return filepath.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(name) == base {
			hits = append(hits, path)
		}
		return nil
	})
	return hits
}

func isUnderDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readTextFile reads a text file with cat -n formatting
func (t *ReadTool) readTextFile(filePath string, offset, limit int) (string, int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() {
		_ = file.Close()
	}()

	if !t.isTextFile(file) {
		return "", 0, fmt.Errorf("%s", ErrorUnreadableBinary)
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
		return "", 0, fmt.Errorf("error reading file %s: %w", filePath, err)
	}

	actualEndLine := 0
	if len(lines) > 0 {
		actualEndLine = offset + len(lines) - 1
	}
	return strings.Join(lines, "\n"), actualEndLine, nil
}

// readPDF reads a PDF file and extracts text with page headers
func (t *ReadTool) readPDF(filePath string, offset, limit int) (string, int, error) {
	file, reader, err := pdf.Open(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to open PDF: %w", err)
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

	actualEndLine := 0
	if len(lines) > 0 {
		actualEndLine = offset + len(lines) - 1
	}
	return strings.Join(lines, "\n"), actualEndLine, nil
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
	return sandbox.ValidateRead(t.config, path)
}

// FormatResult formats tool execution results for different contexts
func (t *ReadTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterUI:
		return t.FormatForUI(result)
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *ReadTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	readResult, ok := result.Data.(*agentdomain.FileReadToolResult)
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

// FormatResultBody returns the file content for the collapsed preview.
func (t *ReadTool) FormatResultBody(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return ""
	}

	readResult, ok := result.Data.(*agentdomain.FileReadToolResult)
	if !ok {
		return ""
	}
	return strings.TrimRight(readResult.Content, "\n")
}

// FormatForUI formats the result for UI display
func (t *ReadTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	args := make(map[string]any, len(result.Arguments))
	for k, v := range result.Arguments {
		args[k] = v
	}
	if fp, ok := args["file_path"].(string); ok {
		args["file_path"] = t.formatter.GetFileName(fp)
	}

	toolCall := t.formatter.FormatToolCall(args, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", toolCall)
	fmt.Fprintf(&output, "└─ %s %s", statusIcon, preview)

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *ReadTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var dataContent string
	if result.Data != nil {
		dataContent = t.formatReadData(result.Data)
	}
	return t.formatter.FormatExpanded(result, dataContent)
}

// formatReadData formats read-specific data
func (t *ReadTool) formatReadData(data any) string {
	readResult, ok := data.(*agentdomain.FileReadToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	fmt.Fprintf(&output, "File: %s\n", readResult.FilePath)

	lineCount := 0
	if readResult.Content != "" {
		lineCount = strings.Count(readResult.Content, "\n") + 1
	}

	if readResult.StartLine > 0 {
		fmt.Fprintf(&output, "Lines: %d", readResult.StartLine)
		if readResult.EndLine > 0 && readResult.EndLine != readResult.StartLine {
			fmt.Fprintf(&output, "-%d", readResult.EndLine)
		}
		output.WriteString("\n")
	}

	fmt.Fprintf(&output, "Lines: %d\n", lineCount)
	fmt.Fprintf(&output, "Size: %d bytes\n", readResult.Size)

	if readResult.Error != "" {
		fmt.Fprintf(&output, "Error: %s\n", readResult.Error)
	}
	if readResult.Content != "" {
		fmt.Fprintf(&output, "Content:\n%s\n", readResult.Content)
	}
	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *ReadTool) ShouldCollapseArg(key string) bool {
	return key == "file_path"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *ReadTool) ShouldAlwaysExpand() bool {
	return false
}
