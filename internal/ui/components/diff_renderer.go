package components

import (
	"fmt"
	"os"
	"strings"

	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// DiffRenderer provides high-performance diff rendering with colors
type DiffRenderer struct {
	styleProvider *styles.Provider
}

// NewDiffRenderer creates a new diff renderer with colored output
func NewDiffRenderer(styleProvider *styles.Provider) *DiffRenderer {
	return &DiffRenderer{
		styleProvider: styleProvider,
	}
}

// RenderEditToolArguments renders Edit tool arguments with diff preview
func (d *DiffRenderer) RenderEditToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)

	var result strings.Builder

	result.WriteString(d.styleProvider.RenderWithColorAndBold(filePath, d.styleProvider.GetThemeColor("accent")))
	result.WriteString("\n")
	if replaceAll {
		result.WriteString(d.styleProvider.RenderDimText("Mode: Replace all occurrences"))
		result.WriteString("\n")
	}
	result.WriteString("\n")

	result.WriteString(d.styleProvider.RenderWithColor(fmt.Sprintf("--- a/%s", filePath), d.styleProvider.GetThemeColor("accent")))
	result.WriteString("\n")
	result.WriteString(d.styleProvider.RenderWithColor(fmt.Sprintf("+++ b/%s", filePath), d.styleProvider.GetThemeColor("accent")))
	result.WriteString("\n")

	cleanedOldString := d.cleanString(oldString)
	cleanedNewString := d.cleanString(newString)

	startLine := d.findLineNumber(filePath, oldString)
	result.WriteString(d.renderUnifiedDiff(cleanedOldString, cleanedNewString, startLine))

	return result.String()
}

// findLineNumber finds the line number where the old string starts in the file
func (d *DiffRenderer) findLineNumber(filePath, oldString string) int {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return 1
	}

	fileContent := string(content)
	cleanedOldString := d.cleanString(oldString)

	index := strings.Index(fileContent, cleanedOldString)
	if index != -1 {
		lineNum := 1
		for i := 0; i < index; i++ {
			if fileContent[i] == '\n' {
				lineNum++
			}
		}
		return lineNum
	}

	fileLines := strings.Split(fileContent, "\n")
	oldLines := strings.Split(cleanedOldString, "\n")

	if len(oldLines) == 0 {
		return 1
	}

	firstOldLine := strings.TrimSpace(oldLines[0])
	if firstOldLine == "" && len(oldLines) > 1 {
		firstOldLine = strings.TrimSpace(oldLines[1])
	}

	for i, fileLine := range fileLines {
		if strings.TrimSpace(fileLine) == firstOldLine {
			if len(oldLines) == 1 {
				return i + 1
			}

			match := true
			for j := 1; j < len(oldLines) && (i+j) < len(fileLines); j++ {
				if strings.TrimSpace(oldLines[j]) != strings.TrimSpace(fileLines[i+j]) {
					match = false
					break
				}
			}

			if match {
				return i + 1
			}
		}
	}

	return 1
}

// cleanString removes line number prefixes from Read tool output
func (d *DiffRenderer) cleanString(s string) string {
	lines := strings.Split(s, "\n")
	var cleanedLines []string

	for _, line := range lines {
		if d.isLineNumberPrefix(line) {
			if cleanedLine, shouldSkip := d.extractContentAfterLineNumber(line); shouldSkip {
				cleanedLines = append(cleanedLines, cleanedLine)
				continue
			}
		}
		cleanedLines = append(cleanedLines, line)
	}

	return strings.Join(cleanedLines, "\n")
}

// isLineNumberPrefix checks if a line starts with a line number prefix pattern
func (d *DiffRenderer) isLineNumberPrefix(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || (line[0] >= '0' && line[0] <= '9'))
}

// extractContentAfterLineNumber extracts content after line number prefix if present
func (d *DiffRenderer) extractContentAfterLineNumber(line string) (string, bool) {
	tabIndex := strings.Index(line, "\t")
	if tabIndex > 0 {
		prefix := line[:tabIndex]
		if d.isValidLineNumberPrefix(prefix) {
			return line[tabIndex+1:], true
		}
	}

	arrowIndex := strings.Index(line, "→")
	if arrowIndex > 0 {
		prefix := line[:arrowIndex]
		if d.isValidLineNumberPrefix(prefix) {
			return line[arrowIndex+len("→"):], true
		}
	}

	return "", false
}

// isValidLineNumberPrefix validates if a prefix contains only spaces and digits
func (d *DiffRenderer) isValidLineNumberPrefix(prefix string) bool {
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

// RenderMultiEditToolArguments renders MultiEdit tool arguments
func (d *DiffRenderer) RenderMultiEditToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	editsInterface := args["edits"]

	var result strings.Builder

	result.WriteString(d.styleProvider.RenderWithColorAndBold(filePath, d.styleProvider.GetThemeColor("accent")))
	result.WriteString("\n\n")

	editsArray, ok := editsInterface.([]any)
	if !ok {
		result.WriteString("Invalid edits format\n")
		return result.String()
	}

	result.WriteString(d.styleProvider.RenderDimText(fmt.Sprintf("Operations: %d edits", len(editsArray))))
	result.WriteString("\n\n")

	for i, editInterface := range editsArray {
		editMap, ok := editInterface.(map[string]any)
		if !ok {
			continue
		}

		oldString, _ := editMap["old_string"].(string)
		newString, _ := editMap["new_string"].(string)
		replaceAll, _ := editMap["replace_all"].(bool)

		result.WriteString(d.styleProvider.RenderWithColor(fmt.Sprintf("Edit %d:", i+1), d.styleProvider.GetThemeColor("accent")))
		result.WriteString("\n")
		if replaceAll {
			result.WriteString(d.styleProvider.RenderDimText("Replace all occurrences"))
			result.WriteString("\n")
		}

		cleanedOldString := d.cleanString(oldString)
		cleanedNewString := d.cleanString(newString)

		startLine := d.findLineNumber(filePath, oldString)
		result.WriteString(d.renderUnifiedDiff(cleanedOldString, cleanedNewString, startLine))

		if i < len(editsArray)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}

// RenderWriteToolArguments renders Write tool arguments with diff for existing files
func (d *DiffRenderer) RenderWriteToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	content, _ := args["content"].(string)

	var result strings.Builder

	existingContent, err := d.readFileIfExists(filePath)
	if err == nil && existingContent != "" {
		diffInfo := DiffInfo{
			FilePath:   filePath,
			OldContent: existingContent,
			NewContent: content,
			Title:      "← File will be overwritten →",
		}
		return d.RenderDiff(diffInfo)
	}

	icon := d.getFileIcon(filePath)
	header := d.styleProvider.RenderWithColorAndBold(icon+" "+filePath, d.styleProvider.GetThemeColor("accent"))

	result.WriteString(d.styleProvider.RenderBordered(header, 80))
	result.WriteString("\n\n")

	opts := styles.StyleOptions{
		Background: d.styleProvider.GetThemeColor("success"),
		Foreground: "#000000",
		Bold:       true,
	}
	newFileBadge := d.styleProvider.RenderStyledText("NEW FILE", opts)

	result.WriteString(newFileBadge)
	result.WriteString("\n\n")

	result.WriteString(d.renderContentPreview(content))

	return result.String()
}

// readFileIfExists attempts to read a file, returning empty string if not exists
func (d *DiffRenderer) readFileIfExists(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// renderContentPreview renders content with line numbers for preview
func (d *DiffRenderer) renderContentPreview(content string) string {
	lines := strings.Split(content, "\n")

	var result strings.Builder
	maxLineNumWidth := len(fmt.Sprintf("%d", len(lines)))
	gutterSep := d.styleProvider.RenderDimText(" │ ")

	for i, line := range lines {
		if i >= 50 {
			remaining := len(lines) - i
			opts := styles.StyleOptions{
				Foreground: d.styleProvider.GetThemeColor("dim"),
				Italic:     true,
			}
			result.WriteString(d.styleProvider.RenderStyledText(fmt.Sprintf("\n... %d more lines ...", remaining), opts))
			break
		}

		lineNumStr := d.styleProvider.RenderDimText(
			fmt.Sprintf("%*d", maxLineNumWidth, i+1))
		result.WriteString(lineNumStr)
		result.WriteString(gutterSep)
		result.WriteString(line)
		if i < len(lines)-1 {
			result.WriteString("\n")
		}
	}

	return d.styleProvider.RenderBordered(result.String(), 80)
}

// RenderDiff renders a unified diff with colors and modern styling
func (d *DiffRenderer) RenderDiff(diffInfo DiffInfo) string {
	var result strings.Builder

	stats := d.calculateDiffStats(diffInfo.OldContent, diffInfo.NewContent)

	if diffInfo.Title != "" {
		opts := styles.StyleOptions{
			Foreground: d.styleProvider.GetThemeColor("accent"),
			Bold:       true,
		}
		result.WriteString(d.styleProvider.RenderStyledText(diffInfo.Title, opts))
		result.WriteString("\n\n")
	}

	result.WriteString(d.renderFileHeader(diffInfo.FilePath, stats))
	result.WriteString("\n\n")

	result.WriteString(d.styleProvider.RenderDimText(fmt.Sprintf("--- a/%s", diffInfo.FilePath)))
	result.WriteString("\n")
	result.WriteString(d.styleProvider.RenderDimText(fmt.Sprintf("+++ b/%s", diffInfo.FilePath)))
	result.WriteString("\n")

	var diffContent string
	switch {
	case diffInfo.OldContent == "" && diffInfo.NewContent != "":
		diffContent = d.renderNewFileContent(diffInfo.NewContent)
	case diffInfo.OldContent != "" && diffInfo.NewContent == "":
		diffContent = d.renderDeletedFileContent(diffInfo.OldContent)
	default:
		cleanedOldContent := d.cleanString(diffInfo.OldContent)
		cleanedNewContent := d.cleanString(diffInfo.NewContent)

		startLine := 1
		if diffInfo.FilePath != "" && diffInfo.OldContent != "" {
			startLine = d.findLineNumber(diffInfo.FilePath, diffInfo.OldContent)
		}

		diffContent = d.renderUnifiedDiff(cleanedOldContent, cleanedNewContent, startLine)
	}

	result.WriteString(d.styleProvider.RenderBordered(diffContent, 100))

	return result.String()
}

// DiffInfo contains information needed to render a diff
type DiffInfo struct {
	FilePath   string
	OldContent string
	NewContent string
	Title      string
}

// NewToolDiffRenderer creates a tool diff renderer (alias for NewDiffRenderer)
func NewToolDiffRenderer(styleProvider *styles.Provider) *DiffRenderer {
	return NewDiffRenderer(styleProvider)
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
	opts := styles.StyleOptions{
		Foreground: d.styleProvider.GetThemeColor("status"),
		Bold:       true,
	}
	result.WriteString(d.styleProvider.RenderStyledText(chunkHeader, opts))
	result.WriteString("\n")

	maxLineNumWidth := len(fmt.Sprintf("%d", len(newLines)))
	gutterSep := d.styleProvider.RenderDimText(" │ ")

	for i, line := range newLines {
		if i < len(newLines)-1 || line != "" {
			lineNumStr := d.styleProvider.RenderDimText(
				fmt.Sprintf("%*d", maxLineNumWidth, i+1))
			result.WriteString(lineNumStr)
			result.WriteString(gutterSep)
			result.WriteString(d.styleProvider.RenderDiffAddition(fmt.Sprintf("+%s", line)))
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
	opts := styles.StyleOptions{
		Foreground: d.styleProvider.GetThemeColor("status"),
		Bold:       true,
	}
	result.WriteString(d.styleProvider.RenderStyledText(chunkHeader, opts))
	result.WriteString("\n")

	maxLineNumWidth := len(fmt.Sprintf("%d", len(oldLines)))
	gutterSep := d.styleProvider.RenderDimText(" │ ")

	for i, line := range oldLines {
		if i < len(oldLines)-1 || line != "" {
			lineNumStr := d.styleProvider.RenderDimText(
				fmt.Sprintf("%*d", maxLineNumWidth, i+1))
			result.WriteString(lineNumStr)
			result.WriteString(gutterSep)
			result.WriteString(d.styleProvider.RenderDiffRemoval(fmt.Sprintf("-%s", line)))
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
	opts := styles.StyleOptions{
		Foreground: d.styleProvider.GetThemeColor("status"),
		Bold:       true,
	}
	result.WriteString(d.styleProvider.RenderStyledText(chunkHeader, opts))
	result.WriteString("\n")

	type diffLine struct {
		oldLineNum int
		newLineNum int
		content    string
		isAdd      bool
		isDelete   bool
		isContext  bool
	}

	var diffLines []diffLine
	oldIdx := 0
	newIdx := 0
	oldLineNum := startLine
	newLineNum := startLine

	for oldIdx < len(oldLines) || newIdx < len(newLines) {
		oldLine := ""
		newLine := ""

		if oldIdx < len(oldLines) {
			oldLine = oldLines[oldIdx]
		}
		if newIdx < len(newLines) {
			newLine = newLines[newIdx]
		}

		if oldLine == newLine {
			diffLines = append(diffLines, diffLine{
				oldLineNum: oldLineNum,
				newLineNum: newLineNum,
				content:    oldLine,
				isContext:  true,
			})
			oldIdx++
			newIdx++
			oldLineNum++
			newLineNum++
		} else if oldIdx >= len(oldLines) {
			diffLines = append(diffLines, diffLine{
				newLineNum: newLineNum,
				content:    newLine,
				isAdd:      true,
			})
			newIdx++
			newLineNum++
		} else if newIdx >= len(newLines) {
			diffLines = append(diffLines, diffLine{
				oldLineNum: oldLineNum,
				content:    oldLine,
				isDelete:   true,
			})
			oldIdx++
			oldLineNum++
		} else {
			diffLines = append(diffLines, diffLine{
				oldLineNum: oldLineNum,
				content:    oldLine,
				isDelete:   true,
			})
			diffLines = append(diffLines, diffLine{
				newLineNum: newLineNum,
				content:    newLine,
				isAdd:      true,
			})
			oldIdx++
			newIdx++
			oldLineNum++
			newLineNum++
		}
	}

	maxLineNumWidth := len(fmt.Sprintf("%d", max(oldLineNum, newLineNum)))
	gutterSep := d.styleProvider.RenderDimText(" │ ")

	for _, line := range diffLines {
		var lineNumStr string
		if line.isDelete {
			lineNumStr = d.styleProvider.RenderDimText(fmt.Sprintf("%*d", maxLineNumWidth, line.oldLineNum))
		} else {
			lineNumStr = d.styleProvider.RenderDimText(fmt.Sprintf("%*d", maxLineNumWidth, line.newLineNum))
		}

		result.WriteString(lineNumStr)
		result.WriteString(gutterSep)

		if line.isContext {
			result.WriteString(d.styleProvider.RenderDimText(fmt.Sprintf(" %s", line.content)))
		} else if line.isAdd {
			result.WriteString(d.styleProvider.RenderDiffAddition(fmt.Sprintf("+%s", line.content)))
		} else if line.isDelete {
			result.WriteString(d.styleProvider.RenderDiffRemoval(fmt.Sprintf("-%s", line.content)))
		}
		result.WriteString("\n")
	}

	output := result.String()
	return strings.TrimSuffix(output, "\n")
}

// DiffStats represents statistics about a diff
type DiffStats struct {
	LinesAdded   int
	LinesRemoved int
	LinesChanged int
}

// calculateDiffStats computes statistics from old and new content
func (d *DiffRenderer) calculateDiffStats(oldContent, newContent string) DiffStats {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	stats := DiffStats{}
	maxLines := max(len(oldLines), len(newLines))

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
			continue
		}

		if oldLine != "" && newLine != "" {
			stats.LinesChanged++
		} else if oldLine == "" {
			stats.LinesAdded++
		} else if newLine == "" {
			stats.LinesRemoved++
		}
	}

	return stats
}

// renderDiffStats creates a visual stats summary
func (d *DiffRenderer) renderDiffStats(stats DiffStats) string {
	if stats.LinesAdded == 0 && stats.LinesRemoved == 0 && stats.LinesChanged == 0 {
		return ""
	}

	var parts []string

	if stats.LinesAdded > 0 {
		parts = append(parts, d.styleProvider.RenderDiffAddition(fmt.Sprintf("+%d", stats.LinesAdded)))
	}
	if stats.LinesRemoved > 0 {
		parts = append(parts, d.styleProvider.RenderDiffRemoval(fmt.Sprintf("-%d", stats.LinesRemoved)))
	}
	if stats.LinesChanged > 0 {
		parts = append(parts, d.styleProvider.RenderWithColor(fmt.Sprintf("~%d", stats.LinesChanged), d.styleProvider.GetThemeColor("status")))
	}

	return d.styleProvider.RenderDimText("Changes: ") + strings.Join(parts, " ")
}

// getFileIcon returns an appropriate icon/glyph for a file based on extension
func (d *DiffRenderer) getFileIcon(filePath string) string {
	ext := strings.ToLower(filePath)

	switch {
	case strings.HasSuffix(ext, ".go"):
		return "🐹"
	case strings.HasSuffix(ext, ".js"), strings.HasSuffix(ext, ".ts"):
		return "📜"
	case strings.HasSuffix(ext, ".py"):
		return "🐍"
	case strings.HasSuffix(ext, ".md"):
		return "📝"
	case strings.HasSuffix(ext, ".json"), strings.HasSuffix(ext, ".yaml"), strings.HasSuffix(ext, ".yml"):
		return "⚙️ "
	case strings.HasSuffix(ext, ".html"), strings.HasSuffix(ext, ".css"):
		return "🌐"
	case strings.HasSuffix(ext, ".rs"):
		return "🦀"
	case strings.HasSuffix(ext, ".java"):
		return "☕"
	case strings.HasSuffix(ext, ".sh"), strings.HasSuffix(ext, ".bash"):
		return "🔧"
	default:
		return "📄"
	}
}

// renderFileHeader creates an elegant file header with metadata
func (d *DiffRenderer) renderFileHeader(filePath string, stats DiffStats) string {
	icon := d.getFileIcon(filePath)
	fileName := d.styleProvider.RenderWithColorAndBold(icon+" "+filePath, d.styleProvider.GetThemeColor("accent"))

	var header strings.Builder
	header.WriteString(fileName)

	statsLine := d.renderDiffStats(stats)
	if statsLine != "" {
		header.WriteString("  ")
		header.WriteString(statsLine)
	}

	return d.styleProvider.RenderBordered(header.String(), 100)
}
