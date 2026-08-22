package output

import (
	"strings"
	"testing"

	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	icons "github.com/inference-gateway/cli/internal/presentation/tui/styles/icons"
)

func TestDisableColorsStripsEscapes(t *testing.T) {
	renderer := NewRenderer()
	renderer.DisableColors()

	span := &telemetry.TraceSpan{Name: "chat", DurationMs: 3000}
	outputs := map[string]string{
		"table": renderer.NewListTable("Tool", "Calls").Row("Bash", "54").Render(),
		"title": renderer.Title("Tool Calls"),
		"field": renderer.Field("Session", "abc"),
		"hint":  renderer.Hint("legend"),
		"tree":  styles.RenderTraceTree([]*telemetry.TraceSpan{span}, renderer.TraceTreeStyle()),
		"check": icons.StyledCheckMark(),
		"cross": icons.StyledCrossMark(),
	}
	for name, rendered := range outputs {
		if strings.Contains(rendered, "\x1b") {
			t.Errorf("%s output still contains ANSI escapes: %q", name, rendered)
		}
	}
}
