package cmd

import (
	"strings"
	"testing"

	telemetry "github.com/inference-gateway/cli/internal/telemetry"
)

// disableOutputColors must leave no ANSI escape sequences in any shared
// command-output rendering path (tables, titles, hints, fields, trace trees).
func TestDisableOutputColorsStripsEscapes(t *testing.T) {
	disableOutputColors()

	span := &telemetry.TraceSpan{Name: "chat", DurationMs: 3000}
	outputs := map[string]string{
		"table": newListTable("Tool", "Calls").Row("Bash", "54").Render(),
		"title": listTitle("Tool Calls"),
		"field": listField("Session", "abc"),
		"hint":  listHint("legend"),
		"tree":  telemetry.RenderTraceTree([]*telemetry.TraceSpan{span}, traceTreeStyle),
	}
	for name, out := range outputs {
		if strings.Contains(out, "\x1b") {
			t.Errorf("%s output still contains ANSI escapes: %q", name, out)
		}
	}
}
