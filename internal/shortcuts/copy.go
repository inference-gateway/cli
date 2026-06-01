package shortcuts

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// ClipboardWriter copies UTF-8 text to the system clipboard. It is defined here
// (rather than in internal/domain) so the CopyShortcut can be unit-tested with a
// hand-written fake without touching the real clipboard or shelling out.
type ClipboardWriter interface {
	Copy(ctx context.Context, text string) error
}

// CopyShortcut copies the current conversation to the system clipboard.
type CopyShortcut struct {
	repo      domain.ConversationRepository
	clipboard ClipboardWriter
}

// NewCopyShortcut creates a new CopyShortcut.
func NewCopyShortcut(repo domain.ConversationRepository, clipboard ClipboardWriter) *CopyShortcut {
	return &CopyShortcut{
		repo:      repo,
		clipboard: clipboard,
	}
}

func (c *CopyShortcut) GetName() string { return "copy" }
func (c *CopyShortcut) GetDescription() string {
	return "Copy the current conversation to the system clipboard"
}
func (c *CopyShortcut) GetUsage() string              { return "/copy [text|markdown|json]" }
func (c *CopyShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (c *CopyShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if c.repo.GetMessageCount() == 0 {
		return ShortcutResult{
			Output:  "No conversation to copy - conversation history is empty",
			Success: true,
		}, nil
	}

	format, err := parseCopyFormat(args)
	if err != nil {
		return ShortcutResult{Output: err.Error(), Success: false}, nil
	}

	data, err := c.repo.Export(format)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to export conversation: %v", err),
			Success: false,
		}, nil
	}

	if err := c.clipboard.Copy(ctx, string(data)); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to copy to clipboard: %v", err),
			Success: false,
		}, nil
	}

	lines := bytes.Count(data, []byte("\n")) + 1
	return ShortcutResult{
		Output: fmt.Sprintf("• Copied conversation to clipboard (%s, %d bytes, %d lines)",
			format, len(data), lines),
		Success:    true,
		SideEffect: SideEffectNone,
	}, nil
}

// parseCopyFormat resolves the optional format argument, defaulting to plain text.
func parseCopyFormat(args []string) (domain.ExportFormat, error) {
	if len(args) == 0 {
		return domain.ExportText, nil
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "text", "txt":
		return domain.ExportText, nil
	case "markdown", "md":
		return domain.ExportMarkdown, nil
	case "json":
		return domain.ExportJSON, nil
	default:
		return "", fmt.Errorf("unknown format %q (use text, markdown, or json)", args[0])
	}
}
