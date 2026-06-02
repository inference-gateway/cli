package diffview

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
)

// chromaFormatter returns a chroma.Formatter that emits ANSI-styled output
// preserving the given background color on every token. Adapted from
// charmbracelet/crush/internal/ui/xchroma for lipgloss v1.
//
// If bgColor is empty, only foreground / weight styling is applied.
func chromaFormatter(bgColor string) chroma.Formatter {
	return chroma.FormatterFunc(func(w io.Writer, style *chroma.Style, it chroma.Iterator) error {
		for token := it(); token != chroma.EOF; token = it() {
			value := strings.TrimRight(token.Value, "\n")

			entry := style.Get(token.Type)
			if entry.IsZero() {
				if _, err := fmt.Fprint(w, value); err != nil {
					return err
				}
				continue
			}

			s := lipgloss.NewStyle()
			if bgColor != "" {
				s = s.Background(lipgloss.Color(bgColor))
			}
			if entry.Bold == chroma.Yes {
				s = s.Bold(true)
			}
			if entry.Italic == chroma.Yes {
				s = s.Italic(true)
			}
			if entry.Underline == chroma.Yes {
				s = s.Underline(true)
			}
			if entry.Colour.IsSet() {
				s = s.Foreground(lipgloss.Color(entry.Colour.String()))
			}

			if _, err := fmt.Fprint(w, s.Render(value)); err != nil {
				return err
			}
		}
		return nil
	})
}
