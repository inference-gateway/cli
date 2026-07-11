package styles

import (
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestProviderStyleCache exercises the lazy per-theme style cache: renders
// must be stable while the theme is unchanged and pick up a SetTheme switch
// on the next call without any explicit invalidation.
func TestProviderStyleCache(t *testing.T) {
	ts := domain.NewThemeProvider()
	p := NewProvider(ts)

	first := p.RenderErrorText("boom")
	if second := p.RenderErrorText("boom"); second != first {
		t.Errorf("consecutive renders differ: %q vs %q", first, second)
	}

	if err := ts.SetTheme("github-light"); err != nil {
		t.Fatal(err)
	}
	switched := p.RenderErrorText("boom")
	if switched == first {
		t.Errorf("render did not change after theme switch: %q", switched)
	}

	if err := ts.SetTheme("tokyo-night"); err != nil {
		t.Fatal(err)
	}
	if back := p.RenderErrorText("boom"); back != first {
		t.Errorf("render after switching back differs from original: %q vs %q", back, first)
	}
}
