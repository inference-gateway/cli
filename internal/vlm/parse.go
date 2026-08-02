// Package vlm provides image annotation (image -> scene summary + numbered
// element list) via a local llama.cpp vision model or a gateway side-call.
// It is gated by config.VisionConfig and mirrors internal/stt: CGO-free,
// shelling out to a local binary and downloading models on demand.
package vlm

import (
	"encoding/json"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// jsonContract is the strict-JSON response instruction appended to every
// annotation prompt. bboxUnit differs per engine: the gateway engine asks for
// pixel coordinates, the local engine for 0-1000-normalized ones (Qwen-VL's
// native grounding convention), rescaled afterwards.
const jsonContract = `Respond with ONLY this JSON, no markdown, no code fences:
{"summary":"<1-2 sentence description>","elements":[{"index":1,"label":"button","text":"Sign in","bbox":[x1,y1,x2,y2]}]}
Omit "elements" when nothing is notable. %s`

// parseAnnotation turns raw model output into an ImageAnnotation. It strips
// code fences and extracts the outermost JSON object; on any failure it
// degrades to a summary-only annotation with the raw text - it never fails.
func parseAnnotation(raw string) *domain.ImageAnnotation {
	text := strings.TrimSpace(raw)

	candidate := text
	if start := strings.Index(candidate, "{"); start >= 0 {
		if end := strings.LastIndex(candidate, "}"); end > start {
			candidate = candidate[start : end+1]
		}
	}

	var annotation domain.ImageAnnotation
	if err := json.Unmarshal([]byte(candidate), &annotation); err == nil && annotation.Summary != "" {
		return &annotation
	}
	return &domain.ImageAnnotation{Summary: text}
}

// rescaleBBoxes maps 0-1000-normalized bounding boxes to pixel space. It is a
// no-op when dimensions are unknown.
func rescaleBBoxes(a *domain.ImageAnnotation, width, height int) {
	if a == nil || width <= 0 || height <= 0 {
		return
	}
	for i := range a.Elements {
		b := &a.Elements[i].BBox
		b[0] = b[0] * width / 1000
		b[1] = b[1] * height / 1000
		b[2] = b[2] * width / 1000
		b[3] = b[3] * height / 1000
	}
}
