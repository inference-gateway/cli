package macos

import (
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestParseAXElements(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []domain.AnnotatedElement
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "single element",
			in:   "AXButton|Save|10|20|30|40\n",
			want: []domain.AnnotatedElement{
				{Index: 1, Label: "button", Text: "Save", BBox: [4]int{10, 20, 40, 60}},
			},
		},
		{
			name: "dock item role split",
			in:   "AXDockItem|Finder|5|900|60|60\n",
			want: []domain.AnnotatedElement{
				{Index: 1, Label: "dock item", Text: "Finder", BBox: [4]int{5, 900, 65, 960}},
			},
		},
		{
			name: "malformed lines skipped",
			in:   "AXButton|OK|1|2|3\nnot-a-line\nAXButton|OK|1|2|x|4\nAXMenuBarItem|File|0|0|50|22\n",
			want: []domain.AnnotatedElement{
				{Index: 1, Label: "menu bar item", Text: "File", BBox: [4]int{0, 0, 50, 22}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAXElements(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("parseAXElements() returned %d elements, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("element %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestNormalizeAXRole(t *testing.T) {
	tests := []struct{ in, want string }{
		{"AXButton", "button"},
		{"AXDockItem", "dock item"},
		{"AXMenuBarItem", "menu bar item"},
		{"AXUnknown", "unknown"},
		{"Weird", "weird"},
	}
	for _, tt := range tests {
		if got := normalizeAXRole(tt.in); got != tt.want {
			t.Errorf("normalizeAXRole(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
