package components

import (
	"fmt"
	"os"
	"strings"

	chroma "github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"

	domain "github.com/inference-gateway/cli/internal/domain"
	diffview "github.com/inference-gateway/cli/internal/ui/components/diffview"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
)

// InlineDiffContextLines is the unchanged-context line count shown above/below
// each change in the *inline* diffs: the Edit approval preview and the post-run
// Edit/MultiEdit diffs in the conversation. The full /diff viewer keeps the wider
// diffview default so it stays a full-file review surface.
const InlineDiffContextLines = 2

// DiffRenderer is a thin adapter that preserves the legacy public surface
// (RenderEditToolArguments / RenderMultiEditToolArguments / RenderWriteToolArguments
// / RenderDiff / RenderColoredDiff) and delegates the actual diff rendering
// to the diffview package. The internal renderer there uses go-udiff for a
// correct LCS-based diff algorithm and optional chroma syntax highlighting.
type DiffRenderer struct {
	styleProvider *styles.Provider
	width         int
	contextLines  int
	maxLines      int
	startLine     int
}

// defaultContentPreviewLines caps the new-file preview so a huge file can't blow
// out a non-scrollable caller. Callers that bound the height themselves (e.g. the
// scrollable approval box) opt out via SetMaxLines(-1).
const defaultContentPreviewLines = 50

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

// SetContextLines overrides the number of unchanged context lines shown above and
// below each change. Pass 0 (the default) to keep the diffview default. Returns
// the renderer for chaining.
func (d *DiffRenderer) SetContextLines(n int) *DiffRenderer { d.contextLines = n; return d }

// SetMaxLines overrides the new-file preview line cap: 0 keeps the default, a
// positive n caps at n, and a negative n renders every line (for callers that
// bound the height themselves, like the scrollable approval box). Chains.
func (d *DiffRenderer) SetMaxLines(n int) *DiffRenderer { d.maxLines = n; return d }

// SetStartLine tells the renderer that the diffed content is a snippet whose
// first line is line n in the real file (1-based), so gutter line numbers show
// the actual file positions instead of counting from 1. Pass 0 (the default)
// when the content is already whole-file. Chains.
func (d *DiffRenderer) SetStartLine(n int) *DiffRenderer { d.startLine = n; return d }

// RenderEditToolArguments renders Edit tool arguments as a diff. When the target
// file can be read and old_string is found in it, it diffs the real file content
// so the configured context lines surround the change with actual neighbouring
// file lines; otherwise it falls back to a snippet diff of old_string vs new_string.
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
	out.WriteString(d.editDiff(filePath, oldString, newString, replaceAll))
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
	out.WriteString(d.styleProvider.RenderWithColorAndBold(filePath, d.styleProvider.GetThemeColor("accent")))
	out.WriteString("\n\n")
	out.WriteString(d.styleProvider.RenderStyledText(" NEW FILE ", styles.StyleOptions{
		Background: d.styleProvider.GetThemeColor("success"),
		Foreground: "#000000",
		Bold:       true,
		Padding:    [2]int{0, 1},
	}))
	out.WriteString("\n\n")
	out.WriteString(d.renderContentPreview(filePath, content))
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

// editDiff renders an Edit as a diff with surrounding file context. When the
// target file is readable and contains old_string, it diffs the real file
// (before) against the file with the replacement applied (after), so the
// renderer's context lines come from the actual file around the change. It falls
// back to a snippet diff (old_string vs new_string) when the file can't be read
// or old_string isn't found verbatim (e.g. a whitespace-flexible match).
func (d *DiffRenderer) editDiff(filePath, oldString, newString string, replaceAll bool) string {
	if before, after, ok := editFileContents(filePath, oldString, newString, replaceAll); ok {
		return d.buildDiffView(filePath, before, after).String()
	}
	return d.snippetDiff(filePath, oldString, newString)
}

// editFileContents reads the target file and applies the replacement, returning
// the before/after file content. ok is false (so the caller falls back to a
// snippet diff) when old_string is empty, the file can't be read, or old_string
// isn't found verbatim.
func editFileContents(filePath, oldString, newString string, replaceAll bool) (before, after string, ok bool) {
	if oldString == "" {
		return "", "", false
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", false
	}
	before = string(content)
	if !strings.Contains(before, oldString) {
		return "", "", false
	}
	after = strings.Replace(before, oldString, newString, 1)
	if replaceAll {
		after = strings.ReplaceAll(before, oldString, newString)
	}
	return before, after, true
}

func snippetStartLine(filePath, oldString string) int {
	if oldString == "" {
		return 0
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0
	}
	before, _, found := strings.Cut(string(content), oldString)
	if !found {
		return 0
	}
	return strings.Count(before, "\n") + 1
}

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
		Style(d.diffStyle()).
		WithChromaStyle(d.chromaStyle())
	if d.contextLines > 0 {
		dv = dv.ContextLines(d.contextLines)
	}
	if d.startLine > 1 {
		dv = dv.LineNumberOffset(d.startLine - 1)
	}
	if d.width > 0 {
		dv = dv.Width(d.width)
	}
	return dv
}

// diffStyle returns a Style derived from the active theme: the light/dark base
// (and its tuned background tints) is chosen by the theme's perceived
// brightness, and the add/remove/accent/dim foreground colours come from the
// theme itself so each theme shows its own diff palette. Falls back to the
// default dark style when no theme is available.
func (d *DiffRenderer) diffStyle() diffview.Style {
	theme := d.themeOrNil()
	if theme == nil {
		return diffview.DefaultDarkStyle()
	}
	return diffview.NewThemeAwareStyle(diffview.ThemePalette{
		Add:    theme.GetDiffAddColor(),
		Remove: theme.GetDiffRemoveColor(),
		Accent: theme.GetAccentColor(),
		Dim:    theme.GetDimColor(),
		Dark:   !isLightTheme(theme),
	})
}

// chromaStyle returns a chroma highlighting style derived from the active
// theme's brightness. Returns nil when no theme is available (no highlighting).
func (d *DiffRenderer) chromaStyle() *chroma.Style {
	theme := d.themeOrNil()
	if theme == nil {
		return nil
	}
	if isLightTheme(theme) {
		return chromastyles.Get("github")
	}
	return chromastyles.Get("github-dark")
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

// themeIsDark reports whether the active theme is dark, defaulting to dark when no
// theme is set. Used to pick the embedded editor's background.
func themeIsDark(sp *styles.Provider) bool {
	theme := sp.GetCurrentTheme()
	return theme == nil || !isLightTheme(theme)
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

func (d *DiffRenderer) renderContentPreview(filePath, content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if cs := d.chromaStyle(); cs != nil {
		highlighted := diffview.Highlight(filePath, content, cs, false)
		lines = strings.Split(highlighted, "\n")
	}

	limit := d.contentPreviewLimit()
	var out strings.Builder
	maxWidth := len(fmt.Sprintf("%d", len(lines)))
	gutter := d.styleProvider.RenderDimText(" │ ")

	for i, line := range lines {
		if limit >= 0 && i >= limit {
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

// contentPreviewLimit resolves the new-file preview cap: 0 → default, <0 →
// unlimited (negative), >0 → that value.
func (d *DiffRenderer) contentPreviewLimit() int {
	if d.maxLines == 0 {
		return defaultContentPreviewLines
	}
	return d.maxLines
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

func (d *DiffRenderer) renderFileHeader(filePath string, stats DiffStats) string {
	fileName := d.styleProvider.RenderWithColorAndBold(filePath, d.styleProvider.GetThemeColor("accent"))

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
