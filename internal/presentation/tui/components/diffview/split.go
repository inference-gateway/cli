package diffview

import (
	"slices"

	"github.com/aymanbagabas/go-udiff"
)

// splitHunk is the side-by-side projection of a unified udiff.Hunk. Each
// splitLine has at most one "before" and one "after" entry; equal lines carry
// the same content on both sides, delete-only lines pair with the next
// matching insert when one exists (to render an in-place modification), and
// otherwise pad with nil on the missing side.
type splitHunk struct {
	fromLine int
	toLine   int
	lines    []*splitLine
}

type splitLine struct {
	before *udiff.Line
	after  *udiff.Line
}

// hunkToSplit converts one unified hunk to its side-by-side representation.
// Adapted from charmbracelet/crush/internal/ui/diffview/split.go.
func hunkToSplit(h *udiff.Hunk) splitHunk {
	lines := slices.Clone(h.Lines)
	sh := splitHunk{
		fromLine: h.FromLine,
		toLine:   h.ToLine,
		lines:    make([]*splitLine, 0, len(lines)),
	}

	for len(lines) > 0 {
		ul := lines[0]
		lines = lines[1:]

		var sl splitLine
		switch ul.Kind {
		case udiff.Equal:
			ulCopy := ul
			sl.before = &ulCopy
			sl.after = &ulCopy
		case udiff.Insert:
			ulCopy := ul
			sl.after = &ulCopy
		case udiff.Delete:
			ulCopy := ul
			sl.before = &ulCopy

			// pair this delete with the next insert if one comes before any
			// equal line - represents an in-place modification
			for i, l := range lines {
				if l.Kind == udiff.Equal {
					break
				}
				if l.Kind == udiff.Insert {
					lCopy := l
					sl.after = &lCopy
					lines = append(lines[:i], lines[i+1:]...)
					break
				}
			}
		}
		slCopy := sl
		sh.lines = append(sh.lines, &slCopy)
	}

	return sh
}
