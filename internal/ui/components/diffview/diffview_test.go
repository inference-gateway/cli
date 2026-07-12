package diffview

import (
	"strings"
	"testing"
)

// TestMidFileInsertNoCascade is the regression test for the bug in the legacy
// renderer: a single-line insertion in the middle of a file used to mark every
// subsequent line as both an add and a delete because the renderer walked old
// and new lines as parallel arrays. go-udiff produces the correct shift.
func TestMidFileInsertNoCascade(t *testing.T) {
	before := strings.Join([]string{
		"line1",
		"line2",
		"line3",
		"line4",
		"line5",
	}, "\n") + "\n"

	after := strings.Join([]string{
		"line1",
		"line2",
		"INSERTED",
		"line3",
		"line4",
		"line5",
	}, "\n") + "\n"

	out := New().
		Before("file.txt", before).
		After("file.txt", after).
		Layout(LayoutUnified).
		String()

	// strip ANSI for content-level assertions
	plain := stripANSI(out)

	if strings.Count(plain, "+") < 2 { // 1 from hunk header @@..@@ and 1 from insert symbol
		t.Fatalf("expected at least one insert marker, got:\n%s", plain)
	}
	if strings.Contains(plain, "- line3") || strings.Contains(plain, "- line4") || strings.Contains(plain, "- line5") {
		t.Fatalf("subsequent lines should not be marked deleted (regression!); got:\n%s", plain)
	}
	if !strings.Contains(plain, "INSERTED") {
		t.Fatalf("expected INSERTED in output, got:\n%s", plain)
	}
}

func TestUnifiedRendersWithLineNumbers(t *testing.T) {
	before := "alpha\nbeta\ngamma\n"
	after := "alpha\nBETA\ngamma\n"

	out := New().
		Before("f", before).
		After("f", after).
		FileName("f").
		Layout(LayoutUnified).
		String()
	plain := stripANSI(out)

	if !strings.Contains(plain, "BETA") {
		t.Fatalf("expected BETA in output:\n%s", plain)
	}
	if !strings.Contains(plain, "@@") {
		t.Fatalf("expected hunk header in output:\n%s", plain)
	}
}

func TestLineNumberOffsetShiftsGutter(t *testing.T) {
	before := "alpha\nbeta\ngamma\n"
	after := "alpha\nBETA\ngamma\n"

	out := New().
		Before("f", before).
		After("f", after).
		Layout(LayoutUnified).
		LineNumberOffset(99).
		String()
	plain := stripANSI(out)

	if !strings.Contains(plain, "101") {
		t.Fatalf("expected offset line number 101 in output:\n%s", plain)
	}
	if strings.Contains(plain, "@@ -2,") || strings.Contains(plain, "@@ -1,") {
		t.Fatalf("hunk header should reflect offset, got:\n%s", plain)
	}
}

func TestAutoLayoutPicksSplitWhenWide(t *testing.T) {
	dv := New().
		Before("f", "a\n").
		After("f", "b\n").
		Width(200) // > defaultSplitMinWidth (160)
	_ = dv.String()
	if dv.resolvedLayout != LayoutSplit {
		t.Fatalf("expected split, got %v", dv.resolvedLayout)
	}
}

func TestAutoLayoutPicksUnifiedWhenNarrow(t *testing.T) {
	dv := New().
		Before("f", "a\n").
		After("f", "b\n").
		Width(80)
	_ = dv.String()
	if dv.resolvedLayout != LayoutUnified {
		t.Fatalf("expected unified, got %v", dv.resolvedLayout)
	}
}

func TestExplicitLayoutOverridesAuto(t *testing.T) {
	dv := New().
		Before("f", "a\n").
		After("f", "b\n").
		Width(200).
		Layout(LayoutUnified)
	_ = dv.String()
	if dv.resolvedLayout != LayoutUnified {
		t.Fatalf("explicit layout ignored: got %v", dv.resolvedLayout)
	}
}

func TestIdenticalContentRendersEmpty(t *testing.T) {
	out := New().Before("f", "x\n").After("f", "x\n").String()
	if stripANSI(out) != "" {
		t.Fatalf("expected empty output for identical content, got: %q", out)
	}
}

// stripANSI removes ANSI escape sequences for plain-text assertions.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' && s[i] != 'K' && s[i] != 'H' && s[i] != 'J' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
