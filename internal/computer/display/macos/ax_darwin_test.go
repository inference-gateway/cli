//go:build darwin

package macos

import (
	"context"
	"testing"
)

// TestAXProviderListDock is a smoke test against the live Dock accessibility
// tree. Skipped when the process lacks the Accessibility permission (CI,
// unconfigured terminals).
func TestAXProviderListDock(t *testing.T) {
	if !hasAccessibilityPermissions() {
		t.Skip("accessibility permission not granted")
	}

	elements, err := axProvider{}.ListElements(context.Background(), "dock")
	if err != nil {
		t.Fatalf("ListElements(dock) error: %v", err)
	}
	if len(elements) == 0 {
		t.Fatal("ListElements(dock) returned no elements")
	}
	for _, el := range elements {
		if el.Text == "" {
			t.Errorf("element %d has empty title: %+v", el.Index, el)
		}
		if el.BBox[2] <= el.BBox[0] || el.BBox[3] <= el.BBox[1] {
			t.Errorf("element %d has degenerate bbox: %+v", el.Index, el)
		}
	}
}
