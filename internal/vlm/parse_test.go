package vlm

import (
	"testing"
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
