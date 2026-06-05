package services

import (
	"fmt"
	"strings"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
)

// previewLineCount is how many output lines the collapsed tool result shows on success.
const previewLineCount = 5

// ResultBodyProvider is an optional interface a tool may implement to expose its
// primary output (command stdout, file content, …) for the collapsed preview and
// the full-on-failure view. It is distinct from FormatPreview (a one-line summary)
// and FormatForLLM (the full tree) and lives in the services package so adding it
// does not touch the domain.Tool interface or its generated mocks.
type ResultBodyProvider interface {
	FormatResultBody(result *domain.ToolExecutionResult) string
}

// resultBody returns the tool's primary output text used for the collapsed preview.
// It prefers a ResultBodyProvider (full, untruncated) and falls back to the tool's
// short FormatPreview summary.
func (s *ToolFormatterService) resultBody(result *domain.ToolExecutionResult) string {
	tool, err := s.toolRegistry.GetTool(result.ToolName)
	if err != nil {
		return ""
	}
	if bp, ok := tool.(ResultBodyProvider); ok {
		if body := bp.FormatResultBody(result); body != "" {
			return strings.TrimRight(body, "\n")
		}
	}
	return strings.TrimRight(tool.FormatPreview(result), "\n")
}

// previewLines splits the body into the lines shown in the collapsed view and how
// many additional lines were hidden. On success it shows the first previewLineCount
// lines; on failure it shows the full body (so errors are immediately visible).
func previewLines(body string, success bool, width int) (lines []string, more int) {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return nil, 0
	}

	all := strings.Split(body, "\n")
	if !success || len(all) <= previewLineCount {
		return capLines(all, width), 0
	}
	return capLines(all[:previewLineCount], width), len(all) - previewLineCount
}

// capLines truncates each line to the available width.
func capLines(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = formatting.TruncateText(ln, width)
	}
	return out
}

// contentWidth is the width available for preview content after reserving room for
// the left indent and a small right buffer.
func contentWidth(terminalWidth int) int {
	w := formatting.GetResponsiveWidth(terminalWidth) - 6
	if w < 20 {
		return 20
	}
	return w
}

// pluralizeLines formats the hidden-line count ("+1 line" / "+3 lines").
func pluralizeLines(n int) string {
	if n == 1 {
		return "+1 line"
	}
	return fmt.Sprintf("+%d lines", n)
}

// formatDurationShort renders a duration with sub-second granularity (e.g. "19ms",
// "1.4s", "2m3s"), matching the expanded tree's Duration field.
func formatDurationShort(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		m := int(d.Minutes())
		sec := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, sec)
	}
}

// collapsedFooter builds the dim footer for the collapsed view, combining the
// hidden-line count with the expand hint ("+1 line · ctrl+o to expand").
func (s *ToolFormatterService) collapsedFooter(more int) string {
	hint := s.expandHint()
	switch {
	case more > 0 && hint != "":
		return pluralizeLines(more) + " · " + hint
	case more > 0:
		return pluralizeLines(more)
	default:
		return hint
	}
}

// collapseHintLine builds the dim "· ctrl+o to collapse" line appended to the
// expanded tree. It is omitted for always-expanded tools (which cannot collapse).
func (s *ToolFormatterService) collapseHintLine(result *domain.ToolExecutionResult) string {
	if s.ShouldAlwaysExpandTool(result.ToolName) {
		return ""
	}
	hint := s.collapseHint()
	if hint == "" {
		return ""
	}
	return s.styleProvider.RenderWithColor("· "+hint, s.styleProvider.GetThemeColor("dim"))
}

// themeTreeLines applies A2A-consistent theming to the raw expanded tree string:
// the tool-call line is accent+bold, tree connectors are dim, field labels are
// accent, and any tool-emitted content (status colour, diffs, raw output) is left
// untouched so it renders exactly as before.
func (s *ToolFormatterService) themeTreeLines(tree string) string {
	tree = strings.TrimRight(tree, "\n")
	if tree == "" {
		return tree
	}
	lines := strings.Split(tree, "\n")
	for i, line := range lines {
		lines[i] = s.themeTreeLine(line, i == 0)
	}
	return strings.Join(lines, "\n")
}

// themeTreeLine themes a single tree line. See themeTreeLines for the colour rules.
func (s *ToolFormatterService) themeTreeLine(line string, isFirst bool) string {
	if isFirst {
		return s.styleProvider.RenderWithColorAndBold(line, s.styleProvider.GetThemeColor("accent"))
	}

	prefix, rest := splitTreePrefix(line)
	if prefix == "" {
		return line
	}

	styledPrefix := s.styleProvider.RenderWithColor(prefix, s.styleProvider.GetThemeColor("dim"))

	if isFieldLine(prefix) {
		if label, value, ok := splitLabel(rest); ok && !containsANSI(label) {
			return styledPrefix + s.styleProvider.RenderWithColor(label, s.styleProvider.GetThemeColor("accent")) + value
		}
	}
	return styledPrefix + rest
}

// splitTreePrefix separates the leading run of indent/connector runes from the rest.
func splitTreePrefix(line string) (prefix, rest string) {
	runes := []rune(line)
	i := 0
	for i < len(runes) && isTreeRune(runes[i]) {
		i++
	}
	return string(runes[:i]), string(runes[i:])
}

func isTreeRune(r rune) bool {
	switch r {
	case ' ', '│', '├', '└', '─':
		return true
	}
	return false
}

// isFieldLine reports whether the prefix denotes a structured field (├─ / └─),
// as opposed to a continuation/body line (spaces or "│ " only).
func isFieldLine(prefix string) bool {
	return strings.Contains(prefix, "├─") || strings.Contains(prefix, "└─")
}

// splitLabel splits "Label: value" at the first colon, keeping the colon on the label.
func splitLabel(s string) (label, value string, ok bool) {
	idx := strings.IndexByte(s, ':')
	if idx < 0 {
		return "", "", false
	}
	return s[:idx+1], s[idx+1:], true
}

func containsANSI(s string) bool {
	return strings.Contains(s, "\x1b[")
}
