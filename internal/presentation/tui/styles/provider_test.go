package styles

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
)

// TestRenderTitledCard checks the card frames content with a rounded border, splices
// the title into the top border, keeps every line the same width, and renders the
// requested content inside.
func TestRenderTitledCard(t *testing.T) {
	p := NewProvider(NewThemeProvider())

	out := p.RenderTitledCard("hello\nworld", "Bash", p.GetThemeColor("border"), p.GetThemeColor("accent"), 40)
	plain := stripSGR(out)
	lines := strings.Split(plain, "\n")

	if len(lines) < 4 {
		t.Fatalf("expected a bordered box, got %d lines: %q", len(lines), plain)
	}
	if !strings.HasPrefix(lines[0], "╭") || !strings.Contains(lines[0], "Bash") {
		t.Errorf("top border missing rounded corner/title: %q", lines[0])
	}
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "╰") || !strings.HasSuffix(last, "╯") {
		t.Errorf("bottom border missing rounded corners: %q", last)
	}
	if !strings.Contains(plain, "hello") || !strings.Contains(plain, "world") {
		t.Errorf("card body missing content: %q", plain)
	}
	w := lipgloss.Width(lines[0])
	for i, ln := range lines {
		if got := lipgloss.Width(ln); got != w {
			t.Errorf("line %d width %d != %d: %q", i, got, w, ln)
		}
	}
}

// TestRenderInputFieldIconColor checks that iconColor colors only the leading
// branch icon, that "" leaves the whole label in the status color, and that the
// colored icon and PR number both survive truncation of a long branch name.
func TestRenderInputFieldIconColor(t *testing.T) {
	p := NewProvider(NewThemeProvider())
	status, warning := p.GetThemeColor("status"), p.GetThemeColor("warning")
	coloredIcon := p.RenderWithColor("⎇", warning)

	tests := []struct {
		name      string
		width     int
		label     string
		iconColor string
		wantIcon  string
		wantText  []string
		wantGone  []string
	}{
		{"default color", 60, "⎇ main", "", p.RenderWithColor(" ⎇ main ", status), nil, []string{coloredIcon}},
		{"colored icon", 60, "⎇ main ─ #12", warning, coloredIcon, []string{p.RenderWithColor(" main", status), "#12"}, nil},
		{"truncated keeps icon and pr", 30, "⎇ feature/this-branch-name-goes-on-and-on ─ #12", warning, coloredIcon, []string{"...", "#12"}, []string{"goes-on-and-on"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			top, _, _ := strings.Cut(p.RenderInputField("> ", tt.width, false, tt.label, tt.iconColor), "\n")
			if !strings.Contains(top, tt.wantIcon) {
				t.Errorf("top border %q missing %q", top, tt.wantIcon)
			}
			for _, w := range tt.wantText {
				if !strings.Contains(top, w) {
					t.Errorf("top border %q missing %q", top, w)
				}
			}
			for _, g := range tt.wantGone {
				if strings.Contains(top, g) {
					t.Errorf("top border %q should not contain %q", top, g)
				}
			}
		})
	}
}

func stripSGR(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case inEsc:
			// skip
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestProviderStyleCache exercises the lazy per-theme style cache: renders
// must be stable while the theme is unchanged and pick up a SetTheme switch
// on the next call without any explicit invalidation.
func TestProviderStyleCache(t *testing.T) {
	ts := NewThemeProvider()
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
