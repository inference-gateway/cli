// Package text provides a CGO-free, cross-platform clipboard text writer that
// shells out to the platform's native clipboard utility (pbcopy, wl-copy,
// xclip, xsel, or clip). It is intentionally separate from the image-focused
// internal/clipboard package, which relies on CGO and is only built on macOS.
package text

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Writer copies text to the system clipboard via native CLI utilities.
type Writer struct{}

// NewWriter creates a new clipboard text Writer.
func NewWriter() *Writer {
	return &Writer{}
}

// candidate is a single clipboard utility invocation (command name + fixed args).
type candidate struct {
	name string
	args []string
}

// Copy writes text to the system clipboard, trying the platform's clipboard
// utilities in order and skipping any that are not installed. It returns an
// actionable error if no working utility is found.
func (w *Writer) Copy(ctx context.Context, text string) error {
	candidates := clipboardCandidates()
	if len(candidates) == 0 {
		return fmt.Errorf("clipboard copy is not supported on %s", runtime.GOOS)
	}

	var lastErr error
	for _, c := range candidates {
		if _, err := exec.LookPath(c.name); err != nil {
			lastErr = err
			continue
		}
		if err := runCopy(ctx, c, text); err != nil {
			lastErr = err
			continue
		}
		return nil
	}

	return fmt.Errorf("no working clipboard utility found (install one of: %s): %w",
		utilNames(candidates), lastErr)
}

// runCopy pipes text to the clipboard utility's stdin.
func runCopy(ctx context.Context, c candidate, text string) error {
	cmd := exec.CommandContext(ctx, c.name, c.args...)
	cmd.Stdin = strings.NewReader(text)

	if out, err := cmd.CombinedOutput(); err != nil {
		trimmed := bytes.TrimSpace(out)
		if len(trimmed) > 0 {
			return fmt.Errorf("%s failed: %w: %s", c.name, err, trimmed)
		}
		return fmt.Errorf("%s failed: %w", c.name, err)
	}
	return nil
}

// clipboardCandidates returns the clipboard utilities to try for the current OS,
// in priority order.
func clipboardCandidates() []candidate {
	switch runtime.GOOS {
	case "darwin":
		return []candidate{{name: "pbcopy"}}
	case "windows":
		return []candidate{{name: "clip"}}
	default: // linux, *bsd, etc.
		return []candidate{
			{name: "wl-copy"}, // Wayland
			{name: "xclip", args: []string{"-selection", "clipboard"}}, // X11
			{name: "xsel", args: []string{"--clipboard", "--input"}},   // X11 alternative
			{name: "clip.exe"}, // WSL -> Windows host
		}
	}
}

// utilNames joins candidate command names for use in error messages.
func utilNames(candidates []candidate) string {
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.name)
	}
	return strings.Join(names, ", ")
}
