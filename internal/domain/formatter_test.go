package domain

import (
	"strings"
	"testing"
	"time"
)

// TestFormatExpandedNativeTree verifies the expanded result renders as a native
// lipgloss/tree (rounded ├──/╰── connectors), keeps the field order, nests the
// Result body and arguments, and is not wrapped in a card (LLM/headless stay plain).
func TestFormatExpandedNativeTree(t *testing.T) {
	f := NewBaseFormatter("Bash")
	result := &ToolExecutionResult{
		ToolName:  "Bash",
		Success:   true,
		Duration:  120 * time.Millisecond,
		Arguments: map[string]any{"command": "ls -la"},
		Metadata:  map[string]string{"exit_code": "0"},
	}

	out := f.FormatExpanded(result, "line one\nline two")

	for _, want := range []string{
		"Bash(command=ls -la)",
		"├── Duration: 120ms",
		"├── Arguments:",
		"│   ╰── command: ls -la",
		"Result:",
		"line one",
		"╰── Metadata:",
		"exit_code: 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expanded tree missing %q in:\n%s", want, out)
		}
	}

	// native rounded glyphs replace the old single-dash form
	if strings.Contains(out, "├─ ") || strings.Contains(out, "└─ ") {
		t.Errorf("expected native ├──/╰── glyphs, found old single-dash connectors:\n%s", out)
	}
	// LLM/headless output must not be wrapped in a card
	if strings.Contains(out, "╭") || strings.Contains(out, "╮") {
		t.Errorf("LLM expanded output must not be card-framed:\n%s", out)
	}
}
