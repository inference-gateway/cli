package shortcuts

import (
	"context"
	"strings"
	"testing"
)

// ShortcutResult.Output is never printed - its only consumer persists it as an
// assistant message (tui/handlers.ChatShortcutHandler). Terminal styling there
// is written straight to storage, where it surfaces as literal escape codes in
// any non-terminal reader. Regression test for inference-gateway/desktop#289.
func TestCustomShortcutOutputCarriesNoTerminalStyling(t *testing.T) {
	c := &CustomShortcut{}
	snippet := &SnippetConfig{Prompt: "describe {x}", Template: "{llm}"}

	tests := []struct {
		name          string
		commandOutput string
	}{
		{name: "snippet generation starts", commandOutput: `{"x":"1"}`},
		{name: "command output is not JSON", commandOutput: "not json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := c.executeWithSnippet(context.Background(), tt.commandOutput, snippet)
			if err != nil {
				t.Fatalf("executeWithSnippet() failed: %v", err)
			}
			if result.Output == "" {
				t.Fatal("expected some output to inspect")
			}
			if strings.ContainsRune(result.Output, 0x1b) {
				t.Errorf("Output carries an ANSI escape sequence: %q", result.Output)
			}
		})
	}
}
