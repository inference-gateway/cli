// Package vlm provides image annotation (image -> scene summary + numbered
// element list) via a side-call to a vision model through the inference
// gateway. Gated by config.VisionConfig. The gateway also serves local
// models (e.g. Ollama), so offline annotation is a gateway concern.
package vlm

import (
	"encoding/json"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	"regexp"
	"strings"
)

// jsonContract is the strict-JSON response instruction appended to every
// annotation prompt.
const jsonContract = `Respond with ONLY this JSON, no markdown, no code fences:
{"summary":"<1-2 sentence description>","elements":[{"index":1,"label":"button","text":"Sign in","bbox":[x1,y1,x2,y2]}]}
Omit "elements" when nothing is notable. %s`

// parseAnnotation turns raw model output into an ImageAnnotation. It strips
// code fences and extracts the outermost JSON object; on any failure it
// degrades to a summary-only annotation with the raw text - it never fails.
func parseAnnotation(raw string) *agentdomain.ImageAnnotation {
	text := strings.TrimSpace(raw)

	candidate := text
	if start := strings.Index(candidate, "{"); start >= 0 {
		if end := strings.LastIndex(candidate, "}"); end > start {
			candidate = candidate[start : end+1]
		}
	}

	var annotation agentdomain.ImageAnnotation
	if err := json.Unmarshal([]byte(candidate), &annotation); err == nil && annotation.Summary != "" {
		return &annotation
	}
	if salvaged := salvageAnnotation(candidate); salvaged != nil {
		return salvaged
	}
	return &agentdomain.ImageAnnotation{Summary: text}
}

var (
	summaryRe = regexp.MustCompile(`"summary"\s*:\s*("(?:[^"\\]|\\.)*")`)
	elementRe = regexp.MustCompile(`\{[^{}]*"bbox"\s*:\s*\[[^\]]*\][^{}]*\}`)
)

// salvageAnnotation recovers what it can from a malformed reply (LLM JSON
// glitches, truncation): the summary string plus every element object that
// unmarshals on its own. Returns nil when nothing usable is found.
func salvageAnnotation(candidate string) *agentdomain.ImageAnnotation {
	annotation := agentdomain.ImageAnnotation{}
	if m := summaryRe.FindStringSubmatch(candidate); m != nil {
		_ = json.Unmarshal([]byte(m[1]), &annotation.Summary)
	}
	for _, raw := range elementRe.FindAllString(candidate, -1) {
		var el agentdomain.AnnotatedElement
		if err := json.Unmarshal([]byte(raw), &el); err == nil {
			annotation.Elements = append(annotation.Elements, el)
		}
	}
	if annotation.Summary == "" && len(annotation.Elements) == 0 {
		return nil
	}
	return &annotation
}
