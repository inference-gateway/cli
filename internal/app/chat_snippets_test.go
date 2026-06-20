package app

import (
	"strings"
	"testing"

	components "github.com/inference-gateway/cli/internal/ui/components"
)

func TestAugmentWithSnippets_AppendsAndGates(t *testing.T) {
	sels := []components.SnippetSelection{{File: "no_such_file_xyz.go", StartLine: 1, EndLine: 1, Annotation: "n"}}

	app := &ChatApplication{}
	if got, appended := app.augmentWithSnippets("hello"); appended || got != "hello" {
		t.Fatalf("no snippets: got (%q,%v), want (\"hello\",false)", got, appended)
	}

	app = &ChatApplication{pendingSnippets: sels}

	if got, appended := app.augmentWithSnippets("/clear"); appended || got != "/clear" {
		t.Fatalf("slash command: got (%q,%v), want (\"/clear\",false)", got, appended)
	}
	if got, appended := app.augmentWithSnippets("!ls"); appended || got != "!ls" {
		t.Fatalf("bash command: got (%q,%v), want (\"!ls\",false)", got, appended)
	}

	got, appended := app.augmentWithSnippets("please refactor")
	if !appended {
		t.Fatal("normal input should append snippets")
	}
	if !strings.HasPrefix(got, "please refactor\n\n") {
		t.Fatalf("appended content should start with the typed input, got %q", got)
	}

	got, appended = app.augmentWithSnippets("")
	if !appended {
		t.Fatal("empty input with snippets should still append")
	}
	if strings.HasPrefix(got, "\n\n") {
		t.Fatalf("empty input should not be prefixed with separators, got %q", got)
	}
}

func TestIsCommandInput(t *testing.T) {
	cases := map[string]bool{
		"/clear":         true,
		"!ls -la":        true,
		"!!":             true,
		"hello":          false,
		"  /not-trimmed": false,
		"":               false,
	}
	for in, want := range cases {
		if got := isCommandInput(in); got != want {
			t.Errorf("isCommandInput(%q) = %v, want %v", in, got, want)
		}
	}
}
