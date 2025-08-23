package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/ui/shared"
)

// DiffRenderer provides high-performance diff rendering with colors
type DiffRenderer struct {
	theme         shared.Theme
	additionStyle lipgloss.Style
	deletionStyle lipgloss.Style
	headerStyle   lipgloss.Style
	fileStyle     lipgloss.Style
	contextStyle  lipgloss.Style
	lineNumStyle  lipgloss.Style
	chunkStyle    lipgloss.Style
}

// NewDiffRenderer creates a new diff renderer with colored output
func NewDiffRenderer(theme shared.Theme) *DiffRenderer {
	return &DiffRenderer{
		theme:         theme,
		additionStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),   // Green for additions
		deletionStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("1")),   // Red for deletions
		headerStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("6")),   // Cyan for headers
		fileStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("4")),   // Blue for file paths
		contextStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("240")), // Gray for context
		lineNumStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("245")), // Dark gray for line numbers
		chunkStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("5")),   // Magenta for chunk headers
	}
}

// RenderEditToolArguments renders Edit tool arguments with diff preview
func (d *DiffRenderer) RenderEditToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)

	var result strings.Builder

	result.WriteString(d.fileStyle.Render(fmt.Sprintf("File: %s", filePath)))
	result.WriteString("\n")
	if replaceAll {
		result.WriteString(d.contextStyle.Render("Mode: Replace all occurrences"))
		result.WriteString("\n")
	}
	result.WriteString("\n")

	result.WriteString(d.headerStyle.Render(fmt.Sprintf("--- a/%s", filePath)))
	result.WriteString("\n")
	result.WriteString(d.headerStyle.Render(fmt.Sprintf("+++ b/%s", filePath)))
	result.WriteString("\n")

	result.WriteString(d.renderUnifiedDiff(oldString, newString, 1))

	return result.String()
}

// RenderMultiEditToolArguments renders MultiEdit tool arguments
func (d *DiffRenderer) RenderMultiEditToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	editsInterface := args["edits"]

	var result strings.Builder

	result.WriteString(d.fileStyle.Render(fmt.Sprintf("File: %s", filePath)))
	result.WriteString("\n\n")

	editsArray, ok := editsInterface.([]any)
	if !ok {
		result.WriteString("Invalid edits format\n")
		return result.String()
	}

	result.WriteString(d.contextStyle.Render(fmt.Sprintf("Operations: %d edits", len(editsArray))))
	result.WriteString("\n\n")

	for i, editInterface := range editsArray {
		editMap, ok := editInterface.(map[string]any)
		if !ok {
			continue
		}

		oldString, _ := editMap["old_string"].(string)
		newString, _ := editMap["new_string"].(string)
		replaceAll, _ := editMap["replace_all"].(bool)

		result.WriteString(d.headerStyle.Render(fmt.Sprintf("Edit %d:", i+1)))
		result.WriteString("\n")
		if replaceAll {
			result.WriteString(d.contextStyle.Render("Replace all occurrences"))
			result.WriteString("\n")
		}
		result.WriteString(d.renderUnifiedDiff(oldString, newString, 1))
		result.WriteString("\n")
	}

	return result.String()
}

// RenderWriteToolArguments renders Write tool arguments
func (d *DiffRenderer) RenderWriteToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	content, _ := args["content"].(string)

	var result strings.Builder

	result.WriteString(d.fileStyle.Render(fmt.Sprintf("File: %s", filePath)))
	result.WriteString("\n\n")
	result.WriteString(d.contextStyle.Render("Content:"))
	result.WriteString("\n")
	result.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		result.WriteString("\n")
	}

	return result.String()
}

// RenderDiff renders a unified diff with colors
func (d *DiffRenderer) RenderDiff(diffInfo DiffInfo) string {
	var result strings.Builder

	if diffInfo.Title != "" {
		result.WriteString(d.headerStyle.Render(fmt.Sprintf("## %s", diffInfo.Title)))
		result.WriteString("\n\n")
	}

	result.WriteString(d.fileStyle.Render(fmt.Sprintf("File: %s", diffInfo.FilePath)))
	result.WriteString("\n\n")

	result.WriteString(d.headerStyle.Render(fmt.Sprintf("--- a/%s", diffInfo.FilePath)))
	result.WriteString("\n")
	result.WriteString(d.headerStyle.Render(fmt.Sprintf("+++ b/%s", diffInfo.FilePath)))
	result.WriteString("\n")

	if diffInfo.OldContent == "" && diffInfo.NewContent != "" {
		newLines := strings.Split(diffInfo.NewContent, "\n")
		chunkHeader := fmt.Sprintf("@@ -0,0 +1,%d @@", len(newLines))
		result.WriteString(d.chunkStyle.Render(chunkHeader))
		result.WriteString("\n")

		for _, line := range newLines {
			result.WriteString(d.additionStyle.Render(fmt.Sprintf("+%s", line)))
			result.WriteString("\n")
		}
	} else if diffInfo.OldContent != "" && diffInfo.NewContent == "" {
		oldLines := strings.Split(diffInfo.OldContent, "\n")
		chunkHeader := fmt.Sprintf("@@ -1,%d +0,0 @@", len(oldLines))
		result.WriteString(d.chunkStyle.Render(chunkHeader))
		result.WriteString("\n")

		for _, line := range oldLines {
			result.WriteString(d.deletionStyle.Render(fmt.Sprintf("-%s", line)))
			result.WriteString("\n")
		}
	} else {
		result.WriteString(d.renderUnifiedDiff(diffInfo.OldContent, diffInfo.NewContent, 1))
	}

	return result.String()
}

// DiffInfo contains information needed to render a diff
type DiffInfo struct {
	FilePath   string
	OldContent string
	NewContent string
	Title      string
}

// NewToolDiffRenderer creates a tool diff renderer (alias for DiffRenderer)
func NewToolDiffRenderer() *DiffRenderer {
	return &DiffRenderer{
		theme:         nil,
		additionStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),   // Green
		deletionStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("1")),   // Red
		headerStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("6")),   // Cyan
		fileStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("4")),   // Blue
		contextStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("240")), // Gray
		lineNumStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("245")), // Dark gray
		chunkStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("5")),   // Magenta
	}
}

// RenderColoredDiff renders a simple diff between old and new content (for compatibility)
func (d *DiffRenderer) RenderColoredDiff(oldContent, newContent string) string {
	diffInfo := DiffInfo{
		FilePath:   "test-file",
		OldContent: oldContent,
		NewContent: newContent,
		Title:      "Diff Test",
	}
	return d.RenderDiff(diffInfo)
}

// renderUnifiedDiff generates a unified diff with line numbers and chunk headers
func (d *DiffRenderer) renderUnifiedDiff(oldContent, newContent string, startLine int) string {
	if oldContent == newContent {
		return ""
	}

	var result strings.Builder
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	oldCount := len(oldLines)
	newCount := len(newLines)

	chunkHeader := fmt.Sprintf("@@ -%d,%d +%d,%d @@", startLine, oldCount, startLine, newCount)
	result.WriteString(d.chunkStyle.Render(chunkHeader))
	result.WriteString("\n")

	maxLines := oldCount
	if newCount > maxLines {
		maxLines = newCount
	}

	for i := 0; i < maxLines; i++ {
		oldLine := ""
		newLine := ""

		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine == newLine {
			result.WriteString(d.contextStyle.Render(fmt.Sprintf(" %s", oldLine)))
			result.WriteString("\n")
		} else {
			if i < len(oldLines) {
				result.WriteString(d.deletionStyle.Render(fmt.Sprintf("-%s", oldLine)))
				result.WriteString("\n")
			}
			if i < len(newLines) {
				result.WriteString(d.additionStyle.Render(fmt.Sprintf("+%s", newLine)))
				result.WriteString("\n")
			}
		}
	}

	return result.String()
}
