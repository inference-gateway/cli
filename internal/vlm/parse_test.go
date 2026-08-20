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
			"missing summary still keeps the elements",
			`{"elements":[{"index":1,"label":"person","bbox":[1,2,3,4]}]}`,
			"", 1,
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

func TestParseAnnotationSalvagesMalformedElements(t *testing.T) {
	raw := `{"summary":"Dock with icons.","elements":[{"index":9,"label":"dock icon","text":"Finder","bbox":[47,101,76,132]},{"index":10,"label":"dock icon","text":"Mail","bbox":[97,101,127,132]},{"index12,"index":12,"label":"dock icon","text":"Photos","bbox":[197,101`
	ann := parseAnnotation(raw)
	if ann.Summary != "Dock with icons." {
		t.Fatalf("summary not salvaged: %q", ann.Summary)
	}
	if len(ann.Elements) != 2 {
		t.Fatalf("expected 2 salvaged elements, got %d: %+v", len(ann.Elements), ann.Elements)
	}
	if ann.Elements[1].Text != "Mail" || ann.Elements[1].BBox[0] != 97 {
		t.Fatalf("unexpected element: %+v", ann.Elements[1])
	}
}
