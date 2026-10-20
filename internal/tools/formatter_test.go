package tools

import (
	"strings"
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// TestFormatExpandedNativeTree verifies the expanded result renders as a native
// lipgloss/tree (rounded ├──/╰── connectors), keeps the field order, nests the
// Result body and arguments, and is not wrapped in a card (LLM/headless stay plain).
func TestFormatExpandedNativeTree(t *testing.T) {
	f := NewBaseFormatter("Bash")
	result := &agentdomain.ToolExecutionResult{
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

	if strings.Contains(out, "├─ ") || strings.Contains(out, "└─ ") {
		t.Errorf("expected native ├──/╰── glyphs, found old single-dash connectors:\n%s", out)
	}
	if strings.Contains(out, "╭") || strings.Contains(out, "╮") {
		t.Errorf("LLM expanded output must not be card-framed:\n%s", out)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("LLM expanded output must be ANSI-free (no color escapes):\n%q", out)
	}
}

// TestFormatExpandedKeepsLineLengths pins that a result body is not right-padded
// to its widest line: lipgloss pads multi-line nodes, and every padded space
// went to the model as prompt content.
func TestFormatExpandedKeepsLineLengths(t *testing.T) {
	f := NewBaseFormatter("Bash")
	result := &agentdomain.ToolExecutionResult{ToolName: "Bash", Success: true, Duration: time.Millisecond}

	out := f.FormatExpanded(result, "short\n"+strings.Repeat("x", 120)+"\nalso short")

	for _, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, " ") {
			t.Fatalf("line keeps trailing padding: %q", line)
		}
	}
	if !strings.Contains(out, "\n    short\n") {
		t.Fatalf("expected the short line at its own length in:\n%s", out)
	}
}
