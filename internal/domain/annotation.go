package domain

import (
	"context"
	"fmt"
	"strings"
)

// DescribeImage returns annotation text for an image a text-only session
// model cannot see, via the given annotator. It is nil-safe and never fails:
// an unconfigured or failing annotator degrades to an omission note.
func DescribeImage(ctx context.Context, annotator ImageAnnotator, prompt string, img ImageAttachment) string {
	if annotator == nil {
		return "[image omitted: model has no vision; configure vision.annotator for text descriptions]"
	}
	annotation, err := annotator.AnnotateImage(ctx, img, AnnotateOptions{Prompt: prompt})
	if err != nil {
		return fmt.Sprintf("[image omitted: model has no vision; annotation failed: %v]", err)
	}
	return AnnotationText(annotation)
}

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
