package text

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestClipboardCandidates_PerOS(t *testing.T) {
	candidates := clipboardCandidates()
	if len(candidates) == 0 {
		t.Fatalf("expected at least one candidate on %s", runtime.GOOS)
	}

	switch runtime.GOOS {
	case "darwin":
		if candidates[0].name != "pbcopy" {
			t.Errorf("darwin should prefer pbcopy, got %q", candidates[0].name)
		}
	case "windows":
		if candidates[0].name != "clip" {
			t.Errorf("windows should use clip, got %q", candidates[0].name)
		}
	default:
		if candidates[0].name != "wl-copy" {
			t.Errorf("linux should prefer wl-copy (Wayland) first, got %q", candidates[0].name)
		}
		names := utilNames(candidates)
		for _, want := range []string{"wl-copy", "xclip", "xsel"} {
			if !strings.Contains(names, want) {
				t.Errorf("linux candidates should include %q, got %q", want, names)
			}
		}
	}
}

func TestUtilNames(t *testing.T) {
	got := utilNames([]candidate{{name: "a"}, {name: "b"}, {name: "c"}})
	if got != "a, b, c" {
		t.Errorf("utilNames = %q, want %q", got, "a, b, c")
	}
}

func TestCopy_NoUtilityFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	w := NewWriter()
	err := w.Copy(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected an error when no clipboard utility is available")
	}
	if !strings.Contains(err.Error(), "no working clipboard utility found") {
		t.Errorf("unexpected error: %v", err)
	}
}
