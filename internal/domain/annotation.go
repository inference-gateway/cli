package domain

import (
	"fmt"
	"strings"
)

// AnnotationText renders an ImageAnnotation as the canonical LLM-facing text:
// a one-line summary followed by the numbered element list with centers and
// bounding boxes. Every consumer (tools, chat, headless) uses this one shape.
func AnnotationText(a *ImageAnnotation) string {
	if a == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Frame summary: %s", strings.TrimSpace(a.Summary))
	if len(a.Elements) > 0 {
		b.WriteString("\nElements:")
		for i, el := range a.Elements {
			index := el.Index
			if index == 0 {
				index = i + 1
			}
			cx := (el.BBox[0] + el.BBox[2]) / 2
			cy := (el.BBox[1] + el.BBox[3]) / 2
			fmt.Fprintf(&b, "\n%d. %s", index, el.Label)
			if el.Text != "" {
				fmt.Fprintf(&b, " %q", el.Text)
			}
			fmt.Fprintf(&b, " - center (%d,%d) bbox [%d,%d,%d,%d]", cx, cy, el.BBox[0], el.BBox[1], el.BBox[2], el.BBox[3])
		}
	}
	return b.String()
}
