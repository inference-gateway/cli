package components

import (
	"fmt"
	"os"
	"strings"

	"github.com/inference-gateway/cli/internal/ui/shared"
)

// DiffRenderer handles rendering colored diffs for tool previews
type DiffRenderer struct {
	theme shared.Theme
}

// NewDiffRenderer creates a new diff renderer
func NewDiffRenderer(theme shared.Theme) *DiffRenderer {
	return &DiffRenderer{
		theme: theme,
	}
}

// RenderEditToolArguments renders Edit tool arguments with a colored diff preview
func (r *DiffRenderer) RenderEditToolArguments(args map[string]any) string {
	var b strings.Builder

	filePath, _ := args["file_path"].(string)
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)

	b.WriteString("Arguments:\n")
	b.WriteString(fmt.Sprintf("  • file_path: %s\n", filePath))
	b.WriteString(fmt.Sprintf("  • replace_all: %v\n", replaceAll))
	b.WriteString("\n")

	b.WriteString("← Test edit for diff verification →\n")
	b.WriteString(r.RenderColoredDiff(oldString, newString))

	return b.String()
}

// RenderMultiEditToolArguments renders MultiEdit tool arguments with a colored diff preview
func (r *DiffRenderer) RenderMultiEditToolArguments(args map[string]any) string {
	var b strings.Builder

	filePath, _ := args["file_path"].(string)
	editsInterface := args["edits"]

	b.WriteString("Arguments:\n")
	b.WriteString(fmt.Sprintf("  • file_path: %s\n", filePath))

	editsArray, ok := editsInterface.([]any)
	if !ok {
		b.WriteString("  • edits: [invalid format]\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("  • edits: %d operations\n", len(editsArray)))
	b.WriteString("\n")

	b.WriteString("Edit Operations:\n")
	for i, editInterface := range editsArray {
		editMap, ok := editInterface.(map[string]any)
		if !ok {
			continue
		}

		oldString, _ := editMap["old_string"].(string)
		newString, _ := editMap["new_string"].(string)
		replaceAll, _ := editMap["replace_all"].(bool)

		b.WriteString(fmt.Sprintf("  %d. ", i+1))
		if replaceAll {
			b.WriteString("[replace_all] ")
		}

		oldPreview := strings.ReplaceAll(oldString, "\n", "\\n")
		newPreview := strings.ReplaceAll(newString, "\n", "\\n")
		if len(oldPreview) > 50 {
			oldPreview = oldPreview[:47] + "..."
		}
		if len(newPreview) > 50 {
			newPreview = newPreview[:47] + "..."
		}

		b.WriteString(fmt.Sprintf("%s\"%s\"%s → %s\"%s\"%s\n",
			r.theme.GetDiffRemoveColor(), oldPreview, "\033[0m",
			r.theme.GetDiffAddColor(), newPreview, "\033[0m"))
	}

	b.WriteString("\n← Simulated diff preview →\n")

	simulatedDiff := r.simulateMultiEditDiff(filePath, editsArray)
	b.WriteString(simulatedDiff)

	return b.String()
}

// RenderColoredDiff creates a colored diff view
func (r *DiffRenderer) RenderColoredDiff(oldContent, newContent string) string {
	if oldContent == newContent {
		return "No changes to display.\n"
	}

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
		return "No changes to display.\n"
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
		r.appendDiffLine(&diff, i, lineNum, oldLines, newLines)
	}

	return diff.String()
}

// appendDiffLine appends a single line to the diff output
func (r *DiffRenderer) appendDiffLine(diff *strings.Builder, i, lineNum int, oldLines, newLines []string) {
	oldExists := i < len(oldLines)
	newExists := i < len(newLines)

	if oldExists && newExists {
		r.appendBothLinesDiff(diff, lineNum, oldLines[i], newLines[i])
		return
	}

	if oldExists {
		fmt.Fprintf(diff, "%s-%3d %s\033[0m\n", r.theme.GetDiffRemoveColor(), lineNum, oldLines[i])
		return
	}

	if newExists {
		fmt.Fprintf(diff, "%s+%3d %s\033[0m\n", r.theme.GetDiffAddColor(), lineNum, newLines[i])
	}
}

// appendBothLinesDiff appends diff lines when both old and new lines exist
func (r *DiffRenderer) appendBothLinesDiff(diff *strings.Builder, lineNum int, oldLine, newLine string) {
	if oldLine != newLine {
		fmt.Fprintf(diff, "%s-%3d %s\033[0m\n", r.theme.GetDiffRemoveColor(), lineNum, oldLine)
		fmt.Fprintf(diff, "%s+%3d %s\033[0m\n", r.theme.GetDiffAddColor(), lineNum, newLine)
	} else {
		fmt.Fprintf(diff, " %3d %s\n", lineNum, oldLine)
	}
}

// simulateMultiEditDiff simulates the multi-edit operation and generates a diff
func (r *DiffRenderer) simulateMultiEditDiff(filePath string, editsArray []any) string {
	originalContent := ""
	if content, err := os.ReadFile(filePath); err == nil {
		originalContent = string(content)
	}

	currentContent := originalContent

	for _, editInterface := range editsArray {
		editMap, ok := editInterface.(map[string]any)
		if !ok {
			continue
		}

		oldString, ok1 := editMap["old_string"].(string)
		newString, ok2 := editMap["new_string"].(string)
		replaceAll, _ := editMap["replace_all"].(bool)

		if !ok1 || !ok2 {
			continue
		}

		if !strings.Contains(currentContent, oldString) {
			return "⚠️  Edit simulation failed: old_string not found after previous edits\n"
		}

		if replaceAll {
			currentContent = strings.ReplaceAll(currentContent, oldString, newString)
		} else {
			count := strings.Count(currentContent, oldString)
			if count > 1 {
				return fmt.Sprintf("⚠️  Edit simulation failed: old_string not unique (%d occurrences)\n", count)
			}
			currentContent = strings.Replace(currentContent, oldString, newString, 1)
		}
	}

	if originalContent == currentContent {
		return "No changes to display.\n"
	}

	return r.RenderColoredDiff(originalContent, currentContent)
}
