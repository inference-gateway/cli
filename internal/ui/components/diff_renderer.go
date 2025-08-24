package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/inference-gateway/cli/internal/ui/shared"
	"github.com/inference-gateway/cli/internal/ui/styles/colors"
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
		additionStyle: lipgloss.NewStyle().Foreground(colors.DiffAddColor.GetLipglossColor()),
		deletionStyle: lipgloss.NewStyle().Foreground(colors.DiffRemoveColor.GetLipglossColor()),
		headerStyle:   lipgloss.NewStyle().Foreground(colors.HeaderColor.GetLipglossColor()),
		fileStyle:     lipgloss.NewStyle().Foreground(colors.AccentColor.GetLipglossColor()).Bold(true),
		contextStyle:  lipgloss.NewStyle().Foreground(colors.DimColor.GetLipglossColor()),
		lineNumStyle:  lipgloss.NewStyle().Foreground(colors.DimColor.GetLipglossColor()),
		chunkStyle:    lipgloss.NewStyle().Foreground(colors.StatusColor.GetLipglossColor()).Bold(true),
	}
}

// RenderEditToolArguments renders Edit tool arguments with diff preview
func (d *DiffRenderer) RenderEditToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)

	var result strings.Builder

	result.WriteString(d.fileStyle.Render(fmt.Sprintf("📄 %s", filePath)))
	result.WriteString("\n")
	if replaceAll {
		result.WriteString(d.contextStyle.Render("🔄 Mode: Replace all occurrences"))
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

	result.WriteString(d.fileStyle.Render(fmt.Sprintf("📄 %s", filePath)))
	result.WriteString("\n\n")

	editsArray, ok := editsInterface.([]any)
	if !ok {
		result.WriteString("Invalid edits format\n")
		return result.String()
	}

	result.WriteString(d.contextStyle.Render(fmt.Sprintf("✏️  Operations: %d edits", len(editsArray))))
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
		if i < len(editsArray)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}

// RenderWriteToolArguments renders Write tool arguments
func (d *DiffRenderer) RenderWriteToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	content, _ := args["content"].(string)

	var result strings.Builder

	result.WriteString(d.fileStyle.Render(fmt.Sprintf("📄 %s", filePath)))
	result.WriteString("\n\n")
	result.WriteString(d.contextStyle.Render("📝 Content:"))
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
		result.WriteString(d.headerStyle.Render(fmt.Sprintf("✨ %s", diffInfo.Title)))
		result.WriteString("\n\n")
	}

	result.WriteString(d.fileStyle.Render(fmt.Sprintf("📄 %s", diffInfo.FilePath)))
	result.WriteString("\n\n")

	result.WriteString(d.headerStyle.Render(fmt.Sprintf("--- a/%s", diffInfo.FilePath)))
	result.WriteString("\n")
	result.WriteString(d.headerStyle.Render(fmt.Sprintf("+++ b/%s", diffInfo.FilePath)))
	result.WriteString("\n")

	switch {
	case diffInfo.OldContent == "" && diffInfo.NewContent != "":
		result.WriteString(d.renderNewFileContent(diffInfo.NewContent))
	case diffInfo.OldContent != "" && diffInfo.NewContent == "":
		result.WriteString(d.renderDeletedFileContent(diffInfo.OldContent))
	default:
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
		additionStyle: lipgloss.NewStyle().Foreground(colors.DiffAddColor.GetLipglossColor()),
		deletionStyle: lipgloss.NewStyle().Foreground(colors.DiffRemoveColor.GetLipglossColor()),
		headerStyle:   lipgloss.NewStyle().Foreground(colors.HeaderColor.GetLipglossColor()),
		fileStyle:     lipgloss.NewStyle().Foreground(colors.AccentColor.GetLipglossColor()).Bold(true),
		contextStyle:  lipgloss.NewStyle().Foreground(colors.DimColor.GetLipglossColor()),
		lineNumStyle:  lipgloss.NewStyle().Foreground(colors.DimColor.GetLipglossColor()),
		chunkStyle:    lipgloss.NewStyle().Foreground(colors.StatusColor.GetLipglossColor()).Bold(true),
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

// renderNewFileContent renders content for a newly created file
func (d *DiffRenderer) renderNewFileContent(newContent string) string {
	var result strings.Builder
	newLines := strings.Split(newContent, "\n")
	chunkHeader := fmt.Sprintf("@@ -0,0 +1,%d @@", len(newLines))
	result.WriteString(d.chunkStyle.Render(chunkHeader))
	result.WriteString("\n")

	for i, line := range newLines {
		if i < len(newLines)-1 || line != "" {
			result.WriteString(d.additionStyle.Render(fmt.Sprintf("+%s", line)))
			result.WriteString("\n")
		}
	}
	return result.String()
}

// renderDeletedFileContent renders content for a deleted file
func (d *DiffRenderer) renderDeletedFileContent(oldContent string) string {
	var result strings.Builder
	oldLines := strings.Split(oldContent, "\n")
	chunkHeader := fmt.Sprintf("@@ -1,%d +0,0 @@", len(oldLines))
	result.WriteString(d.chunkStyle.Render(chunkHeader))
	result.WriteString("\n")

	for i, line := range oldLines {
		if i < len(oldLines)-1 || line != "" {
			result.WriteString(d.deletionStyle.Render(fmt.Sprintf("-%s", line)))
			result.WriteString("\n")
		}
	}
	return result.String()
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

	maxLines := max(oldCount, newCount)

	for i := range maxLines {
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

	// Remove trailing newline
	output := result.String()
	return strings.TrimSuffix(output, "\n")
}
