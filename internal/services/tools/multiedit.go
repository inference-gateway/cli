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

// MultiEditTool handles multiple exact string replacements in a single file atomically
type MultiEditTool struct {
	config    *config.Config
	enabled   bool
	registry  ReadToolTracker
	formatter domain.BaseFormatter
}

// NewMultiEditTool creates a new multi-edit tool
func NewMultiEditTool(cfg *config.Config) *MultiEditTool {
	return &MultiEditTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Edit.Enabled,
		formatter: domain.NewBaseFormatter("MultiEdit"),
	}
}

// NewMultiEditToolWithRegistry creates a new multi-edit tool with a registry for read tracking
func NewMultiEditToolWithRegistry(cfg *config.Config, registry ReadToolTracker) *MultiEditTool {
	return &MultiEditTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Edit.Enabled,
		registry:  registry,
		formatter: domain.NewBaseFormatter("MultiEdit"),
	}
}

// Definition returns the tool definition for the LLM
func (t *MultiEditTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name: "MultiEdit",
		Description: `This is a tool for making multiple edits to a single file in one operation. It is built on top of the Edit tool and allows you to perform multiple find-and-replace operations efficiently. Prefer this tool over the Edit tool when you need to make multiple edits to the same file.

Before using this tool:

1. Use the Read tool to understand the file's contents and context
2. Verify the directory path is correct

To make multiple file edits, provide the following:
1. file_path: The absolute path to the file to modify (must be absolute, not relative)
2. edits: An array of edit operations to perform, where each edit contains:
   - old_string: The text to replace (must match the file contents exactly, including all whitespace and indentation)
   - new_string: The edited text to replace the old_string
   - replace_all: Replace all occurences of old_string. This parameter is optional and defaults to false.

IMPORTANT:
- All edits are applied in sequence, in the order they are provided
- Each edit operates on the result of the previous edit
- All edits must be valid for the operation to succeed - if any edit fails, none will be applied
- This tool is ideal when you need to make several changes to different parts of the same file
- For Jupyter notebooks (.ipynb files), use the NotebookEdit instead

CRITICAL REQUIREMENTS:
1. All edits follow the same requirements as the single Edit tool
2. The edits are atomic - either all succeed or none are applied
3. Plan your edits carefully to avoid conflicts between sequential operations

WARNING:
- The tool will fail if edits.old_string doesn't match the file contents exactly (including whitespace)
- The tool will fail if edits.old_string and edits.new_string are the same
- Since edits are applied in sequence, ensure that earlier edits don't affect the text that later edits are trying to find

When making edits:
- Ensure all edits result in idiomatic, correct code
- Do not leave the code in a broken state
- Always use absolute file paths (starting with /)
- Only use emojis if the user explicitly requests it. Avoid adding emojis to files unless asked.
- Use replace_all for replacing and renaming strings across the file. This parameter is useful if you want to rename a variable for instance.

If you want to create a new file, use:
- A new file path, including dir name if needed
- First edit: empty old_string and the new file's contents as new_string
- Subsequent edits: normal edit operations on the created content`,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{
					"type":        "string",
					"description": "The absolute path to the file to modify",
				},
				"edits": map[string]any{
					"type":        "array",
					"description": "Array of edit operations to perform sequentially on the file",
					"minItems":    1,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"old_string": map[string]any{
								"type":        "string",
								"description": "The text to replace",
							},
							"new_string": map[string]any{
								"type":        "string",
								"description": "The text to replace it with",
							},
							"replace_all": map[string]any{
								"type":        "boolean",
								"description": "Replace all occurences of old_string (default false).",
								"default":     false,
							},
						},
						"required": []string{"old_string", "new_string"},
					},
				},
			},
			"required": []string{"file_path", "edits"},
		},
	}
}

// Execute runs the multi-edit tool with given arguments
func (t *MultiEditTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("multi-edit tool is not enabled")
	}

	if t.registry != nil && !t.registry.IsReadToolUsed() {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "MultiEdit tool requires that the Read tool has been used at least once in the conversation before editing files",
		}, nil
	}

	filePath, ok := args["file_path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path parameter is required and must be a string",
		}, nil
	}

	editsInterface, ok := args["edits"]
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "edits parameter is required",
		}, nil
	}

	editsArray, ok := editsInterface.([]any)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "edits parameter must be an array",
		}, nil
	}

	if len(editsArray) == 0 {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "edits array must contain at least one edit operation",
		}, nil
	}

	// Parse and validate edits
	edits, err := t.parseEdits(editsArray)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	multiEditResult, err := t.executeMultiEdit(filePath, edits)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "MultiEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "MultiEdit",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      multiEditResult,
	}

	return result, nil
}

// EditOperation represents a single edit operation
type EditOperation struct {
	OldString  string
	NewString  string
	ReplaceAll bool
}

// parseEdits converts the any array to EditOperation structs
func (t *MultiEditTool) parseEdits(editsArray []any) ([]EditOperation, error) {
	var edits []EditOperation

	for i, editInterface := range editsArray {
		editMap, ok := editInterface.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edit at index %d must be an object", i)
		}

		oldString, ok := editMap["old_string"].(string)
		if !ok {
			return nil, fmt.Errorf("old_string parameter is required and must be a string at edit index %d", i)
		}

		newString, ok := editMap["new_string"].(string)
		if !ok {
			return nil, fmt.Errorf("new_string parameter is required and must be a string at edit index %d", i)
		}

		if oldString == newString {
			return nil, fmt.Errorf("new_string must be different from old_string at edit index %d", i)
		}

		replaceAll := false
		if replaceAllVal, exists := editMap["replace_all"]; exists {
			if val, ok := replaceAllVal.(bool); ok {
				replaceAll = val
			}
		}

		edits = append(edits, EditOperation{
			OldString:  oldString,
			NewString:  newString,
			ReplaceAll: replaceAll,
		})
	}

	return edits, nil
}

// Validate checks if the multi-edit tool arguments are valid
func (t *MultiEditTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("multi-edit tool is not enabled")
	}

	if t.registry != nil && !t.registry.IsReadToolUsed() {
		return fmt.Errorf("multi-edit tool requires that the Read tool has been used at least once in the conversation before editing files")
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

	editsInterface, ok := args["edits"]
	if !ok {
		return fmt.Errorf("edits parameter is required")
	}

	editsArray, ok := editsInterface.([]any)
	if !ok {
		return fmt.Errorf("edits parameter must be an array")
	}

	if len(editsArray) == 0 {
		return fmt.Errorf("edits array must contain at least one edit operation")
	}

	// Validate each edit operation
	_, err := t.parseEdits(editsArray)
	if err != nil {
		return err
	}

	return nil
}

// IsEnabled returns whether the multi-edit tool is enabled
func (t *MultiEditTool) IsEnabled() bool {
	return t.enabled
}

// executeMultiEdit performs the actual multi-edit operation atomically
func (t *MultiEditTool) executeMultiEdit(filePath string, edits []EditOperation) (*domain.MultiEditToolResult, error) {
	if err := t.validateFile(filePath); err != nil {
		return nil, err
	}

	var originalContentStr string
	var originalSize int64

	// Check if file exists
	originalContent, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist - start with empty content for new file creation
			originalContentStr = ""
			originalSize = 0
		} else {
			return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
	} else {
		originalContentStr = string(originalContent)
		originalSize = int64(len(originalContent))
	}

	currentContent := originalContentStr

	var editResults []domain.EditOperationResult
	successfulEdits := 0

	for i, edit := range edits {
		cleanedOldString := t.cleanString(edit.OldString)
		if !strings.Contains(currentContent, cleanedOldString) {
			return nil, t.createMultiEditMatchError(currentContent, cleanedOldString, filePath, i+1)
		}

		var newContent string
		var replacedCount int

		if edit.ReplaceAll {
			newContent = strings.ReplaceAll(currentContent, cleanedOldString, edit.NewString)
			replacedCount = strings.Count(currentContent, cleanedOldString)
		} else {
			count := strings.Count(currentContent, cleanedOldString)
			if count > 1 {
				return nil, fmt.Errorf("edit %d failed: old_string '%s' is not unique in file %s (found %d occurrences after previous edits). Use replace_all=true to replace all occurrences or provide a larger string with more surrounding context to make it unique", i+1, cleanedOldString, filePath, count)
			}
			newContent = strings.Replace(currentContent, cleanedOldString, edit.NewString, 1)
			replacedCount = 1
		}

		currentContent = newContent
		successfulEdits++

		editResults = append(editResults, domain.EditOperationResult{
			OldString:     cleanedOldString,
			NewString:     edit.NewString,
			ReplaceAll:    edit.ReplaceAll,
			ReplacedCount: replacedCount,
			Success:       true,
		})
	}

	fileModified := false
	if currentContent != originalContentStr {
		if err := os.WriteFile(filePath, []byte(currentContent), 0644); err != nil {
			return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
		fileModified = true
	}

	newSize := int64(len(currentContent))
	bytesDifference := newSize - originalSize

	result := &domain.MultiEditToolResult{
		FilePath:        filePath,
		Edits:           editResults,
		TotalEdits:      len(edits),
		SuccessfulEdits: successfulEdits,
		FileModified:    fileModified,
		OriginalSize:    originalSize,
		NewSize:         newSize,
		BytesDifference: bytesDifference,
	}

	return result, nil
}

// validatePathSecurity checks if a path is allowed for editing within the sandbox
func (t *MultiEditTool) validatePathSecurity(path string) error {
	return t.config.ValidatePathInSandbox(path)
}

// validateFile checks if a file path is valid - supports both existing files and new file creation
func (t *MultiEditTool) validateFile(path string) error {
	if err := t.validatePathSecurity(path); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			// For new file creation, just ensure the directory exists
			return nil
		}
		return fmt.Errorf("cannot access file %s: %w", path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("path %s is a directory, not a file", path)
	}

	return nil
}

// cleanString removes common artifacts from Read tool output like line number prefixes
func (t *MultiEditTool) cleanString(s string) string {
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
func (t *MultiEditTool) isLineNumberPrefix(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || (line[0] >= '0' && line[0] <= '9'))
}

// extractContentAfterLineNumber extracts content after line number prefix if present
func (t *MultiEditTool) extractContentAfterLineNumber(line string) (string, bool) {
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
func (t *MultiEditTool) isValidLineNumberPrefix(prefix string) bool {
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

// createMultiEditMatchError provides detailed error information when string matching fails in MultiEdit
func (t *MultiEditTool) createMultiEditMatchError(content, searchString, filePath string, editIndex int) error {
	lines := strings.Split(content, "\n")
	searchLines := strings.Split(searchString, "\n")

	suggestions := t.findPotentialMatches(lines, searchLines)
	errorMsg := fmt.Sprintf("edit %d failed: old_string not found in file %s (after previous edits)", editIndex, filePath)

	if len(suggestions) > 0 {
		errorMsg += "\n\nPossible matches found:"
		for _, suggestion := range suggestions {
			errorMsg += "\n\n" + suggestion
		}
		errorMsg += "\n\nHint: Earlier edits may have changed the content. Ensure the text still matches exactly after previous modifications."
	} else {
		errorMsg += "\n\nNo similar content found. Previous edits may have modified the content you're trying to match."
	}

	return fmt.Errorf("%s", errorMsg)
}

// findPotentialMatches searches for potential matches in the content (MultiEdit version)
func (t *MultiEditTool) findPotentialMatches(lines, searchLines []string) []string {
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

// createSuggestion creates a context suggestion for a potential match (MultiEdit version)
func (t *MultiEditTool) createSuggestion(lines, searchLines []string, startLine int) string {
	start := startLine
	end := startLine + len(searchLines)
	if end > len(lines) {
		end = len(lines)
	}

	context := strings.Join(lines[start:end], "\n")
	return fmt.Sprintf("Near line %d:\n%s", start+1, context)
}

// FormatResult formats tool execution results for different contexts
func (t *MultiEditTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *MultiEditTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	multiEditResult, ok := result.Data.(*domain.MultiEditToolResult)
	if !ok {
		if result.Success {
			return "Multi-edit completed successfully"
		}
		return "Multi-edit failed"
	}

	fileName := t.formatter.GetFileName(multiEditResult.FilePath)

	if multiEditResult.FileModified {
		return fmt.Sprintf("Applied %d/%d edits to %s (%d bytes difference)",
			multiEditResult.SuccessfulEdits, multiEditResult.TotalEdits, fileName, multiEditResult.BytesDifference)
	}

	return fmt.Sprintf("No changes needed in %s", fileName)
}

// FormatForUI formats the result for UI display
func (t *MultiEditTool) FormatForUI(result *domain.ToolExecutionResult) string {
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
func (t *MultiEditTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatMultiEditData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatMultiEditData formats multi-edit-specific data
func (t *MultiEditTool) formatMultiEditData(data any) string {
	multiEditResult, ok := data.(*domain.MultiEditToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("File: %s\n", multiEditResult.FilePath))
	output.WriteString(fmt.Sprintf("Total Edits: %d\n", multiEditResult.TotalEdits))
	output.WriteString(fmt.Sprintf("Successful Edits: %d\n", multiEditResult.SuccessfulEdits))
	output.WriteString(fmt.Sprintf("File Modified: %t\n", multiEditResult.FileModified))
	output.WriteString(fmt.Sprintf("Original Size: %d bytes\n", multiEditResult.OriginalSize))
	output.WriteString(fmt.Sprintf("New Size: %d bytes\n", multiEditResult.NewSize))
	output.WriteString(fmt.Sprintf("Bytes Difference: %+d\n", multiEditResult.BytesDifference))

	if len(multiEditResult.Edits) > 0 {
		output.WriteString("\nEdit Operations:\n")
		for i, edit := range multiEditResult.Edits {
			status := "✓"
			if !edit.Success {
				status = "✗"
			}

			oldPreview := t.formatter.TruncateText(edit.OldString, 30)
			newPreview := t.formatter.TruncateText(edit.NewString, 30)

			output.WriteString(fmt.Sprintf("  %d. %s %s → %s", i+1, status, oldPreview, newPreview))

			if edit.ReplacedCount > 0 {
				output.WriteString(fmt.Sprintf(" (%d replacements)", edit.ReplacedCount))
			}

			if edit.Error != "" {
				output.WriteString(fmt.Sprintf(" [Error: %s]", edit.Error))
			}

			output.WriteString("\n")
		}
	}

	return output.String()
}
