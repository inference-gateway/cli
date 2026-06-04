package components

import (
	"fmt"
	"os"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/ui/components/diffview"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// DiffRenderer is a thin adapter that preserves the legacy public surface
// (RenderEditToolArguments / RenderMultiEditToolArguments / RenderWriteToolArguments
// / RenderDiff / RenderColoredDiff) and delegates the actual diff rendering
// to the diffview package. The internal renderer there uses go-udiff for a
// correct LCS-based diff algorithm and optional chroma syntax highlighting.
type DiffRenderer struct {
	styleProvider *styles.Provider
	width         int
}

// DiffInfo carries the inputs for a single diff render.
type DiffInfo struct {
	FilePath   string
	OldContent string
	NewContent string
	Title      string
}

// NewDiffRenderer creates a new diff renderer with colored output.
func NewDiffRenderer(styleProvider *styles.Provider) *DiffRenderer {
	return &DiffRenderer{styleProvider: styleProvider}
}

// NewToolDiffRenderer creates a tool diff renderer (alias for NewDiffRenderer).
func NewToolDiffRenderer(styleProvider *styles.Provider) *DiffRenderer {
	return NewDiffRenderer(styleProvider)
}

// SetWidth tells the renderer the available terminal width so the underlying
// DiffView can pick a width-adaptive layout (split when >= 160 cols).
func (d *DiffRenderer) SetWidth(w int) *DiffRenderer { d.width = w; return d }

// RenderEditToolArguments renders Edit tool arguments as a snippet diff.
func (d *DiffRenderer) RenderEditToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	oldString, _ := args["old_string"].(string)
	newString, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)

	var out strings.Builder
	out.WriteString(d.styleProvider.RenderWithColorAndBold(filePath, d.styleProvider.GetThemeColor("accent")))
	out.WriteString("\n")
	if replaceAll {
		out.WriteString(d.styleProvider.RenderDimText("Mode: Replace all occurrences"))
		out.WriteString("\n")
	}
	out.WriteString("\n")
	out.WriteString(d.snippetDiff(filePath, oldString, newString))
	return out.String()
}

// RenderMultiEditToolArguments renders MultiEdit tool arguments as a sequence
// of snippet diffs (one per edit).
func (d *DiffRenderer) RenderMultiEditToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	editsAny := args["edits"]

	var out strings.Builder
	out.WriteString(d.styleProvider.RenderWithColorAndBold(filePath, d.styleProvider.GetThemeColor("accent")))
	out.WriteString("\n\n")

	edits, ok := editsAny.([]any)
	if !ok {
		out.WriteString("Invalid edits format\n")
		return out.String()
	}

	out.WriteString(d.styleProvider.RenderDimText(fmt.Sprintf("Operations: %d edits", len(edits))))
	out.WriteString("\n\n")

	for i, raw := range edits {
		em, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		oldString, _ := em["old_string"].(string)
		newString, _ := em["new_string"].(string)
		replaceAll, _ := em["replace_all"].(bool)

		out.WriteString(d.styleProvider.RenderWithColor(fmt.Sprintf("Edit %d:", i+1), d.styleProvider.GetThemeColor("accent")))
		out.WriteString("\n")
		if replaceAll {
			out.WriteString(d.styleProvider.RenderDimText("Replace all occurrences"))
			out.WriteString("\n")
		}
		out.WriteString(d.snippetDiff(filePath, oldString, newString))
		if i < len(edits)-1 {
			out.WriteString("\n")
		}
	}

	return out.String()
}

// RenderWriteToolArguments renders Write tool arguments. If the target file
// already exists, the rendered output is a full diff against the new content;
// otherwise it's a NEW FILE badge plus a numbered preview of the first lines.
func (d *DiffRenderer) RenderWriteToolArguments(args map[string]any) string {
	filePath, _ := args["file_path"].(string)
	content, _ := args["content"].(string)

	if existing, err := os.ReadFile(filePath); err == nil && len(existing) > 0 {
		return d.RenderDiff(DiffInfo{
			FilePath:   filePath,
			OldContent: string(existing),
			NewContent: content,
			Title:      "← File will be overwritten →",
		})
	}

	var out strings.Builder
	icon := d.getFileIcon(filePath)
	out.WriteString(d.styleProvider.RenderWithColorAndBold(icon+" "+filePath, d.styleProvider.GetThemeColor("accent")))
	out.WriteString("\n\n")
	out.WriteString(d.styleProvider.RenderStyledText(" NEW FILE ", styles.StyleOptions{
		Background: d.styleProvider.GetThemeColor("success"),
		Foreground: "#000000",
		Bold:       true,
		Padding:    [2]int{0, 1},
	}))
	out.WriteString("\n\n")
	out.WriteString(d.renderContentPreview(content))
	return out.String()
}

// RenderDiff renders a full diff between OldContent and NewContent with a
// title, file header with addition/removal stats, and the underlying DiffView
// body. Empty contents are handled gracefully (new file, deleted file).
func (d *DiffRenderer) RenderDiff(info DiffInfo) string {
	var out strings.Builder

	if info.Title != "" {
		out.WriteString(d.styleProvider.RenderStyledText(info.Title, styles.StyleOptions{
			Foreground: d.styleProvider.GetThemeColor("accent"),
			Bold:       true,
		}))
		out.WriteString("\n\n")
	}

	stats := d.calculateDiffStats(info.OldContent, info.NewContent)
	out.WriteString(d.renderFileHeader(info.FilePath, stats))
	out.WriteString("\n\n")

	out.WriteString(d.styleProvider.RenderDimText(fmt.Sprintf("--- a/%s", info.FilePath)))
	out.WriteString("\n")
	out.WriteString(d.styleProvider.RenderDimText(fmt.Sprintf("+++ b/%s", info.FilePath)))
	out.WriteString("\n")

	out.WriteString(d.buildDiffView(info.FilePath, info.OldContent, info.NewContent).String())
	return out.String()
}

// RenderColoredDiff is a minimal compatibility wrapper that renders a simple
// diff with a placeholder title and file path.
func (d *DiffRenderer) RenderColoredDiff(oldContent, newContent string) string {
	return d.RenderDiff(DiffInfo{
		FilePath:   "test-file",
		OldContent: oldContent,
		NewContent: newContent,
		Title:      "Diff Test",
	})
}

// --- internals ---

func (d *DiffRenderer) snippetDiff(filePath, oldString, newString string) string {
	return d.buildDiffView(filePath, ensureTrailingNL(oldString), ensureTrailingNL(newString)).String()
}

func ensureTrailingNL(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func (d *DiffRenderer) buildDiffView(filePath, before, after string) *diffview.DiffView {
	dv := diffview.New().
		Before(filePath, before).
		After(filePath, after).
		Style(d.diffStyle())
	if d.width > 0 {
		dv = dv.Width(d.width)
	}
	return dv
}

// diffStyle returns a Style chosen based on the active theme's perceived
// background brightness — light themes get the light style, dark the dark.
func (d *DiffRenderer) diffStyle() diffview.Style {
	theme := d.themeOrNil()
	if theme != nil && isLightTheme(theme) {
		return diffview.DefaultLightStyle()
	}
	return diffview.DefaultDarkStyle()
}

func (d *DiffRenderer) themeOrNil() domain.Theme {
	if d.styleProvider == nil {
		return nil
	}
	return d.styleProvider.GetCurrentTheme()
}

// isLightTheme classifies the theme by the luminance of its assistant
// (primary text) color. Dark themes have light text (high luminance); light
// themes have dark text (low luminance).
func isLightTheme(theme domain.Theme) bool {
	return hexLuminance(theme.GetAssistantColor()) < 0.5
}

// hexLuminance returns the relative luminance of a "#RRGGBB" string in [0,1].
// Anything unparseable returns 1 (treated as light text → dark theme path).
func hexLuminance(hex string) float64 {
	hex = strings.TrimPrefix(strings.TrimSpace(hex), "#")
	if len(hex) != 6 {
		return 1
	}
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil {
		return 1
	}
	return (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 255.0
}

func (d *DiffRenderer) renderContentPreview(content string) string {
	lines := strings.Split(content, "\n")
	var out strings.Builder
	maxWidth := len(fmt.Sprintf("%d", len(lines)))
	gutter := d.styleProvider.RenderDimText(" │ ")

	for i, line := range lines {
		if i >= 50 {
			remaining := len(lines) - i
			out.WriteString(d.styleProvider.RenderStyledText(
				fmt.Sprintf("\n... %d more lines ...", remaining),
				styles.StyleOptions{Foreground: d.styleProvider.GetThemeColor("dim"), Italic: true},
			))
			break
		}
		out.WriteString(d.styleProvider.RenderDimText(fmt.Sprintf("%*d", maxWidth, i+1)))
		out.WriteString(gutter)
		out.WriteString(line)
		if i < len(lines)-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}

// DiffStats represents statistics about a diff.
type DiffStats struct {
	LinesAdded   int
	LinesRemoved int
	LinesChanged int
}

func (d *DiffRenderer) calculateDiffStats(oldContent, newContent string) DiffStats {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")
	stats := DiffStats{}
	maxLines := max(len(oldLines), len(newLines))
	for i := range maxLines {
		oldLine, newLine := "", ""
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}
		if oldLine == newLine {
			continue
		}
		switch {
		case oldLine != "" && newLine != "":
			stats.LinesChanged++
		case oldLine == "":
			stats.LinesAdded++
		case newLine == "":
			stats.LinesRemoved++
		}
	}
	return stats
}

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

func (d *DiffRenderer) renderFileHeader(filePath string, stats DiffStats) string {
	icon := d.getFileIcon(filePath)
	fileName := d.styleProvider.RenderWithColorAndBold(icon+" "+filePath, d.styleProvider.GetThemeColor("accent"))

	var content strings.Builder
	content.WriteString(fileName)
	if statsLine := d.renderDiffStats(stats); statsLine != "" {
		content.WriteString("  ")
		content.WriteString(statsLine)
	}

	sepWidth := max(d.styleProvider.GetWidth(content.String()), 40)

	var header strings.Builder
	header.WriteString(d.styleProvider.RenderDimText(strings.Repeat("─", sepWidth)))
	header.WriteString("\n")
	header.WriteString(content.String())
	header.WriteString("\n")
	header.WriteString(d.styleProvider.RenderDimText(strings.Repeat("─", sepWidth)))
	return header.String()
}
