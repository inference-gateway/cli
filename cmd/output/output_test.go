package output

import (
	"bytes"
	"strings"
	"testing"

	cobra "github.com/spf13/cobra"

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

func TestAddFormatFlagListsTheDefaultFormat(t *testing.T) {
	cmd := &cobra.Command{}
	AddFormatFlag(cmd, "text", "json")

	flag := cmd.Flags().Lookup("format")
	if flag.Usage != "Output format (text, json)" {
		t.Errorf("usage = %q, want the default listed first", flag.Usage)
	}
	if flag.DefValue != "text" || flag.Shorthand != "f" {
		t.Errorf("default = %q, shorthand = %q, want text and f", flag.DefValue, flag.Shorthand)
	}
}

func TestPrintJSONIndentsAndEndsWithNewline(t *testing.T) {
	var out bytes.Buffer
	if err := PrintJSON(&out, map[string]int{"total": 1}); err != nil {
		t.Fatalf("PrintJSON: %v", err)
	}
	if want := "{\n  \"total\": 1\n}\n"; out.String() != want {
		t.Errorf("PrintJSON wrote %q, want %q", out.String(), want)
	}
}
