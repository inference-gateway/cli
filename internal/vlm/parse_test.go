package vlm

import (
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestParseAnnotation(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantSummary  string
		wantElements int
	}{
		{
			"clean json",
			`{"summary":"Login form","elements":[{"index":1,"label":"button","text":"Sign in","bbox":[10,20,30,40]}]}`,
			"Login form", 1,
		},
		{
			"fenced json",
			"```json\n{\"summary\":\"Login form\",\"elements\":[]}\n```",
			"Login form", 0,
		},
		{
			"json with surrounding noise",
			"loading model...\n{\"summary\":\"A cat\"}\ndone",
			"A cat", 0,
		},
		{
			"garbage degrades to summary",
			"The image shows a warehouse aisle.",
			"The image shows a warehouse aisle.", 0,
		},
		{
			"empty summary degrades to raw",
			`{"elements":[{"index":1,"label":"person","bbox":[1,2,3,4]}]}`,
			`{"elements":[{"index":1,"label":"person","bbox":[1,2,3,4]}]}`, 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := parseAnnotation(tt.raw)
			if a.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", a.Summary, tt.wantSummary)
			}
			if len(a.Elements) != tt.wantElements {
				t.Errorf("len(Elements) = %d, want %d", len(a.Elements), tt.wantElements)
			}
		})
	}
}

func TestRescaleBBoxes(t *testing.T) {
	a := &domain.ImageAnnotation{Elements: []domain.AnnotatedElement{
		{BBox: [4]int{500, 500, 1000, 1000}},
	}}
	rescaleBBoxes(a, 1024, 768)
	if got, want := a.Elements[0].BBox, [4]int{512, 384, 1024, 768}; got != want {
		t.Errorf("BBox = %v, want %v", got, want)
	}

	unchanged := &domain.ImageAnnotation{Elements: []domain.AnnotatedElement{{BBox: [4]int{1, 2, 3, 4}}}}
	rescaleBBoxes(unchanged, 0, 0)
	if got, want := unchanged.Elements[0].BBox, [4]int{1, 2, 3, 4}; got != want {
		t.Errorf("BBox = %v, want %v (no-op for unknown dims)", got, want)
	}

	rescaleBBoxes(nil, 100, 100) // must not panic
}
