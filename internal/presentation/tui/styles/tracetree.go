package styles

import (
	"fmt"
	"strings"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	tree "charm.land/lipgloss/v2/tree"

	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// TreeStyle colorizes the rendered tree's connectors, durations and
// [error: ...] markers. The zero value renders plain text, which suits markdown
// and code-fence output.
type TreeStyle struct {
	Enumerator lipgloss.Style
	Duration   lipgloss.Style
	Error      lipgloss.Style
}

// enumeratorWidth is the rendered width of one tree indent level ("├── ").
const enumeratorWidth = 4

// RenderTraceTree renders the span tree via lipgloss/v2/tree with durations
// right-aligned in a column. Failed spans carry a trailing
// [error: <type> - <message>] marker, benign end-of-stream conditions carry
// none (see spanErrorText).
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
	svc, kind := s.Attributes["service.name"], s.Attributes["span.kind"]
	switch {
	case svc != "" && kind != "":
		name += " [" + svc + ", " + kind + "]"
	case svc != "":
		name += " [" + svc + "]"
	case kind != "":
		name += " [" + kind + "]"
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
