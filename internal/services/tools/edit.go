package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// EditTool handles exact string replacements in files with strict safety rules
type EditTool struct {
	config    *config.Config
	enabled   bool
	registry  ReadToolTracker
	formatter domain.BaseFormatter
}

// ReadToolTracker interface for tracking read tool usage
type ReadToolTracker interface {
	IsReadToolUsed() bool
}

// NewEditTool creates a new edit tool
func NewEditTool(cfg *config.Config) *EditTool {
	return &EditTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Edit.Enabled,
		formatter: domain.NewBaseFormatter("Edit"),
	}
}

// NewEditToolWithRegistry creates a new edit tool with a registry for read tracking
func NewEditToolWithRegistry(cfg *config.Config, registry ReadToolTracker) *EditTool {
	return &EditTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Edit.Enabled,
		registry:  registry,
		formatter: domain.NewBaseFormatter("Edit"),
	}
}

// Definition returns the tool definition for the LLM
func (t *EditTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name: "Edit",
		Description: `Performs exact string replacements in files.

Usage:
- You must use your Read tool at least once in the conversation before editing. This tool will error if you attempt an edit without reading the file.
- When editing text from Read tool output, ensure you preserve the exact indentation (tabs/spaces) as it appears AFTER the line number prefix. The line number prefix format is: spaces + line number + tab. Everything after that tab is the actual file content to match. Never include any part of the line number prefix in the old_string or new_string.
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- Only use emojis if the user explicitly requests it. Avoid adding emojis to files unless asked.
- The edit will FAIL if old_string is not unique in the file. Either provide a larger string with more surrounding context to make it unique or use replace_all to change every instance of old_string.
- Use replace_all for replacing and renaming strings across the file. This parameter is useful if you want to rename a variable for instance.`,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "The absolute path to the file to modify",
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "The text to replace",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "The text to replace it with (must be different from old_string)",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "Replace all occurrences of old_string (default false)",
					"default":     false,
				},
			},
			"required": []string{"file_path", "old_string", "new_string"},
		},
	}
}

// Execute runs the edit tool with given arguments
func (t *EditTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("edit tool is not enabled")
	}

	if t.registry != nil && !t.registry.IsReadToolUsed() {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "Edit tool requires that the Read tool has been used at least once in the conversation before editing files",
		}, nil
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path parameter is required and must be a string",
		}, nil
	}

	oldString, ok := args["old_string"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "old_string parameter is required and must be a string",
		}, nil
	}

	newString, ok := args["new_string"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "new_string parameter is required and must be a string",
		}, nil
	}

	if oldString == newString {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "new_string must be different from old_string",
		}, nil
	}

	replaceAll := false
	if replaceAllVal, exists := args["replace_all"]; exists {
		if val, ok := replaceAllVal.(bool); ok {
			replaceAll = val
		}
	}

	editResult, err := t.executeEdit(filePath, oldString, newString, replaceAll)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "Edit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Edit",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      editResult,
	}

	return result, nil
}

// Validate checks if the edit tool arguments are valid
func (t *EditTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("edit tool is not enabled")
	}

	if t.registry != nil && !t.registry.IsReadToolUsed() {
		return fmt.Errorf("edit tool requires that the Read tool has been used at least once in the conversation before editing files")
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

	oldString, ok := args["old_string"].(string)
	if !ok {
		return fmt.Errorf("old_string parameter is required and must be a string")
	}

	if oldString == "" {
		return fmt.Errorf("old_string cannot be empty")
	}

	newString, ok := args["new_string"].(string)
	if !ok {
		return fmt.Errorf("new_string parameter is required and must be a string")
	}

	if oldString == newString {
		return fmt.Errorf("new_string must be different from old_string")
	}

	if replaceAll, exists := args["replace_all"]; exists {
		if _, ok := replaceAll.(bool); !ok {
			return fmt.Errorf("replace_all parameter must be a boolean")
		}
	}

	return nil
}

// IsEnabled returns whether the edit tool is enabled
func (t *EditTool) IsEnabled() bool {
	return t.enabled
}

// executeEdit performs the actual edit operation
func (t *EditTool) executeEdit(filePath, oldString, newString string, replaceAll bool) (*domain.EditToolResult, error) {
	if err := t.validateFile(filePath); err != nil {
		return nil, err
	}

	originalContent, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	originalContentStr := string(originalContent)
	originalSize := int64(len(originalContent))

	oldString = t.cleanString(oldString)
	if !strings.Contains(originalContentStr, oldString) {
		return nil, t.createMatchError(originalContentStr, oldString, filePath)
	}

	var newContent string
	var replacedCount int

	if replaceAll {
		newContent = strings.ReplaceAll(originalContentStr, oldString, newString)
		replacedCount = strings.Count(originalContentStr, oldString)
	} else {
		count := strings.Count(originalContentStr, oldString)
		if count > 1 {
			return nil, fmt.Errorf("old_string '%s' is not unique in file %s (found %d occurrences). Use replace_all=true to replace all occurrences or provide a larger string with more surrounding context to make it unique", oldString, filePath, count)
		}
		newContent = strings.Replace(originalContentStr, oldString, newString, 1)
		replacedCount = 1
	}

	fileModified := false
	if newContent != originalContentStr {
		if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
		fileModified = true
	}

	newSize := int64(len(newContent))
	bytesDifference := newSize - originalSize

	diff := generateDiff(originalContentStr, newContent)

	result := &domain.EditToolResult{
		FilePath:        filePath,
		OldString:       oldString,
		NewString:       newString,
		ReplacedCount:   replacedCount,
		ReplaceAll:      replaceAll,
		FileModified:    fileModified,
		OriginalSize:    originalSize,
		NewSize:         newSize,
		BytesDifference: bytesDifference,
		Diff:            diff,
	}

	return result, nil
}

// cleanString removes common artifacts from Read tool output like line number prefixes
func (t *EditTool) cleanString(s string) string {
	lines := strings.Split(s, "\n")
	var cleanedLines []string

	for _, line := range lines {
		if t.isLineNumberPrefix(line) {
			if cleanedLine, shouldSkip := t.extractContentAfterLineNumber(line); shouldSkip {
				cleanedLines = append(cleanedLines, cleanedLine)
				continue
			}
		}
		cleanedLines = append(cleanedLines, line)
	}

	return strings.Join(cleanedLines, "\n")
}

// isLineNumberPrefix checks if a line starts with a line number prefix pattern
func (t *EditTool) isLineNumberPrefix(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || (line[0] >= '0' && line[0] <= '9'))
}

// extractContentAfterLineNumber extracts content after line number prefix if present
func (t *EditTool) extractContentAfterLineNumber(line string) (string, bool) {
	tabIndex := strings.Index(line, "\t")
	if tabIndex <= 0 {
		return "", false
	}

	prefix := line[:tabIndex]
	if t.isValidLineNumberPrefix(prefix) {
		return line[tabIndex+1:], true
	}

	return "", false
}

// isValidLineNumberPrefix validates if a prefix contains only spaces and digits
func (t *EditTool) isValidLineNumberPrefix(prefix string) bool {
	hasDigit := false

	for _, r := range prefix {
		if r >= '0' && r <= '9' {
			hasDigit = true
		} else if r != ' ' && r != '→' {
			return false
		}
	}

	return hasDigit
}

// createMatchError provides detailed error information when string matching fails
func (t *EditTool) createMatchError(content, searchString, filePath string) error {
	lines := strings.Split(content, "\n")
	searchLines := strings.Split(searchString, "\n")

	suggestions := t.findPotentialMatches(lines, searchLines)
	errorMsg := fmt.Sprintf("old_string not found in file %s", filePath)

	if len(suggestions) > 0 {
		errorMsg += "\n\nPossible matches found:"
		for _, suggestion := range suggestions {
			errorMsg += "\n\n" + suggestion
		}
		errorMsg += "\n\nHint: Ensure exact whitespace and indentation match. Use the Read tool to see the current file content."
	} else {
		errorMsg += "\n\nNo similar content found. Please verify the content exists and matches exactly (including whitespace)."
	}

	return fmt.Errorf("%s", errorMsg)
}

// findPotentialMatches searches for potential matches in the content
func (t *EditTool) findPotentialMatches(lines, searchLines []string) []string {
	var suggestions []string

	if len(searchLines) == 0 {
		return suggestions
	}

	firstSearchLine := strings.TrimSpace(searchLines[0])
	if len(firstSearchLine) <= 10 {
		return suggestions
	}

	for i, line := range lines {
		if strings.Contains(strings.TrimSpace(line), firstSearchLine) {
			suggestions = append(suggestions, t.createSuggestion(lines, searchLines, i))

			if len(suggestions) >= 3 {
				break
			}
		}
	}

	return suggestions
}

// createSuggestion creates a context suggestion for a potential match
func (t *EditTool) createSuggestion(lines, searchLines []string, startLine int) string {
	start := startLine
	end := startLine + len(searchLines)
	if end > len(lines) {
		end = len(lines)
	}

	context := strings.Join(lines[start:end], "\n")
	return fmt.Sprintf("Near line %d:\n%s", start+1, context)
}

func generateDiff(oldContent, newContent string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	var diff strings.Builder
	maxLines := len(oldLines)
	if len(newLines) > maxLines {
		maxLines = len(newLines)
	}

	firstChanged := -1
	lastChanged := -1
	for i := 0; i < maxLines; i++ {
		oldLine := ""
		newLine := ""
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			if firstChanged == -1 {
				firstChanged = i
			}
			lastChanged = i
		}
	}

	if firstChanged == -1 {
		return ""
	}

	contextBefore := 3
	contextAfter := 3
	startLine := firstChanged - contextBefore
	if startLine < 0 {
		startLine = 0
	}
	endLine := lastChanged + contextAfter
	if endLine >= maxLines {
		endLine = maxLines - 1
	}

	for i := startLine; i <= endLine; i++ {
		lineNum := i + 1
		appendDiffLine(&diff, i, lineNum, oldLines, newLines)
	}

	return diff.String()
}

func appendDiffLine(diff *strings.Builder, i, lineNum int, oldLines, newLines []string) {
	oldExists := i < len(oldLines)
	newExists := i < len(newLines)

	if oldExists && newExists {
		appendBothLinesDiff(diff, lineNum, oldLines[i], newLines[i])
		return
	}

	if oldExists {
		fmt.Fprintf(diff, "-%3d %s\n", lineNum, oldLines[i])
		return
	}

	if newExists {
		fmt.Fprintf(diff, "+%3d %s\n", lineNum, newLines[i])
	}
}

func appendBothLinesDiff(diff *strings.Builder, lineNum int, oldLine, newLine string) {
	if oldLine != newLine {
		fmt.Fprintf(diff, "-%3d %s\n", lineNum, oldLine)
		fmt.Fprintf(diff, "+%3d %s\n", lineNum, newLine)
	} else {
		fmt.Fprintf(diff, " %3d %s\n", lineNum, oldLine)
	}
}

// validatePathSecurity checks if a path is allowed for editing within the sandbox
func (t *EditTool) validatePathSecurity(path string) error {
	return t.config.ValidatePathInSandbox(path)
}

// validateFile checks if a file path is valid and exists (only works with existing files)
func (t *EditTool) validateFile(path string) error {
	if err := t.validatePathSecurity(path); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file %s does not exist. Edit tool only works with existing files", path)
		}
		return fmt.Errorf("cannot access file %s: %w", path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("path %s is a directory, not a file", path)
	}

	return nil
}

// FormatResult formats tool execution results for different contexts
func (t *EditTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *EditTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	editResult, ok := result.Data.(*domain.EditToolResult)
	if !ok {
		if result.Success {
			return "Edit completed successfully"
		}
		return "Edit failed"
	}

	fileName := t.formatter.GetFileName(editResult.FilePath)

	if editResult.ReplaceAll {
		return fmt.Sprintf("Replaced %d occurrences in %s", editResult.ReplacedCount, fileName)
	}

	if editResult.FileModified {
		return fmt.Sprintf("Updated %s (%d bytes difference)", fileName, editResult.BytesDifference)
	}

	return fmt.Sprintf("No changes needed in %s", fileName)
}

// FormatForUI formats the result for UI display
func (t *EditTool) FormatForUI(result *domain.ToolExecutionResult) string {
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
func (t *EditTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatEditData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *EditTool) ShouldCollapseArg(key string) bool {
	// Collapse old_string and new_string arguments as they can be very long
	return key == "old_string" || key == "new_string"
}

// formatEditData formats edit-specific data
func (t *EditTool) formatEditData(data any) string {
	editResult, ok := data.(*domain.EditToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("File: %s\n", editResult.FilePath))
	output.WriteString(fmt.Sprintf("Replaced Count: %d\n", editResult.ReplacedCount))
	output.WriteString(fmt.Sprintf("Replace All: %t\n", editResult.ReplaceAll))
	output.WriteString(fmt.Sprintf("File Modified: %t\n", editResult.FileModified))
	output.WriteString(fmt.Sprintf("Original Size: %d bytes\n", editResult.OriginalSize))
	output.WriteString(fmt.Sprintf("New Size: %d bytes\n", editResult.NewSize))
	output.WriteString(fmt.Sprintf("Bytes Difference: %+d\n", editResult.BytesDifference))

	if editResult.Diff != "" {
		output.WriteString(fmt.Sprintf("Diff:\n%s\n", editResult.Diff))
	}

	return output.String()
}
