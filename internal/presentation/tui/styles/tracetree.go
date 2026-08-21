// Package styles: lipgloss rendering of telemetry span trees. Lives in the
// presentation layer so internal/platform/telemetry stays otel-only.

package styles

import (
	"fmt"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	tree "charm.land/lipgloss/v2/tree"

	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// TreeStyle colorizes the rendered tree segments. The zero value renders
// plain text, which suits markdown/code-fence output.
type TreeStyle struct {
	Enumerator lipgloss.Style // tree connector glyphs
	Duration   lipgloss.Style
	Error      lipgloss.Style // the [error: ...] marker
}

// enumeratorWidth is the rendered width of one tree indent level ("├── ").
const enumeratorWidth = 4

// RenderTraceTree renders the span tree via lipgloss/v2/tree with durations
// right-aligned in a column:
//
//	session (standard, success)          42.1s
//	├── chat deepseek/deepseek-v4-flash   3.2s
//	╰── execute_tool Bash                27.5s
//
// Failed spans carry a trailing [error: <type>] marker.
func RenderTraceTree(roots []*telemetry.TraceSpan, style TreeStyle) string {
	width := 0
	var measure func(s *telemetry.TraceSpan, depth int)
	measure = func(s *telemetry.TraceSpan, depth int) {
		width = max(width, depth*enumeratorWidth+lipgloss.Width(spanLabel(s)))
		for _, c := range s.Children {
			measure(c, depth+1)
		}
	}
	for _, r := range roots {
		measure(r, 0)
	}

	var node func(s *telemetry.TraceSpan, depth int) *tree.Tree
	node = func(s *telemetry.TraceSpan, depth int) *tree.Tree {
		label := spanLabel(s)
		pad := strings.Repeat(" ", width-depth*enumeratorWidth-lipgloss.Width(label)+2)
		value := label + pad + style.Duration.Render(fmt.Sprintf("%7s", formatSpanDuration(s.DurationMs)))
		if s.Error != "" {
			value += style.Error.Render(" [error: " + s.Error + "]")
		}
		n := tree.Root(value)
		for _, c := range s.Children {
			n.Child(node(c, depth+1))
		}
		return n
	}

	var b strings.Builder
	for _, r := range roots {
		b.WriteString(node(r, 0).
			Enumerator(tree.RoundedEnumerator).
			EnumeratorStyle(style.Enumerator.PaddingRight(1)).
			IndenterStyle(style.Enumerator.PaddingRight(1)).
			String())
		b.WriteString("\n")
	}
	return b.String()
}

// spanLabel decorates the session root with its agent mode and run outcome,
// mirroring the resource-level facts a reader wants at the top of the tree.
// Tool spans include the tool call ID (gen_ai.tool.call.id) when present.
func spanLabel(s *telemetry.TraceSpan) string {
	name := s.Name
	if svc := s.Attributes["service.name"]; svc != "" {
		name += " [" + svc + "]"
	}
	if toolCallID := s.Attributes["gen_ai.tool.call.id"]; toolCallID != "" {
		name += " " + toolCallID
	}
	mode, outcome := s.Attributes["infer.agent.mode"], s.Attributes["infer.run.outcome"]
	if mode != "" && outcome != "" {
		return fmt.Sprintf("%s (%s, %s)", name, mode, outcome)
	}
	return name
}

// formatSpanDuration renders a span duration compactly: 117µs, 41ms, 3.2s, 1m32s.
func formatSpanDuration(ms float64) string {
	d := time.Duration(ms * float64(time.Millisecond))
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return d.Round(time.Second).String()
	}
}
