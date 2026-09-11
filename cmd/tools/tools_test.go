package tools

import (
	"testing"

	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

func TestCanonicalToolName(t *testing.T) {
	available := []string{"Read", "Bash", "WebSearch"}

	cases := map[string]string{
		"read":      "Read",
		"READ":      "Read",
		"Read":      "Read",
		"bash":      "Bash",
		"websearch": "WebSearch",
		"WebSearch": "WebSearch",
		"unknown":   "unknown",
	}

	for in, want := range cases {
		if got := canonicalToolName(available, in); got != want {
			t.Errorf("canonicalToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderToolResultStripsANSIWhenColorsDisabled(t *testing.T) {
	const styled = "\x1b[38;2;1;2;3m│\x1b[m ok"
	utils.SetColorsDisabled(false)
	if got := renderToolResult(styled); got != styled {
		t.Fatalf("colors enabled: got %q", got)
	}
	utils.SetColorsDisabled(true)
	t.Cleanup(func() { utils.SetColorsDisabled(false) })
	if got := renderToolResult(styled); got != "│ ok" {
		t.Fatalf("colors disabled: got %q", got)
	}
}
