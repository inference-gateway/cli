package macos

// Pure-Go half of the accessibility backend (no build tag so it is testable
// on every platform; the cgo bridge lives in ax_darwin.go).

import (
	"strconv"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// parseAXElements parses the pipe-delimited "role|title|x|y|w|h" lines
// emitted by the cgo bridge into annotated elements with 1-based indexes.
// BBox is [x1,y1,x2,y2] in logical screen points. Malformed lines are
// skipped.
func parseAXElements(s string) []domain.AnnotatedElement {
	var elements []domain.AnnotatedElement
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		parts := strings.SplitN(line, "|", 6)
		if len(parts) != 6 {
			continue
		}
		x, errX := strconv.Atoi(parts[2])
		y, errY := strconv.Atoi(parts[3])
		w, errW := strconv.Atoi(parts[4])
		h, errH := strconv.Atoi(parts[5])
		if errX != nil || errY != nil || errW != nil || errH != nil {
			continue
		}
		elements = append(elements, domain.AnnotatedElement{
			Index: len(elements) + 1,
			Label: normalizeAXRole(parts[0]),
			Text:  parts[1],
			BBox:  [4]int{x, y, x + w, y + h},
		})
	}
	return elements
}

// normalizeAXRole turns an AX role constant into a readable label:
// "AXDockItem" -> "dock item", "AXButton" -> "button".
func normalizeAXRole(role string) string {
	role = strings.TrimPrefix(role, "AX")
	var b strings.Builder
	for i, r := range role {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
