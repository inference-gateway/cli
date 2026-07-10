package inputsyntax

import (
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
)

// fakeColor echoes the color key so assertions can check which theme color a
// token received without dealing with real ANSI.
func fakeColor(key string) string { return key }

// fakeRender wraps text in an unambiguous, ANSI-free marker so output is easy to
// assert and to feed back through Highlight (proving already-styled tokens are
// not re-matched).
func fakeRender(text, color string) string { return "<" + color + ">" + text + "</>" }

func knownSet(names ...string) func(string) bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return func(name string) bool {
		if parts := strings.SplitN(name, ":", 2); len(parts) == 2 {
			return set[parts[1]]
		}
		return set[name]
	}
}

func newSkillHighlighter(known ...string) *Highlighter {
	return New([]Rule{SkillRule(knownSet(known...))}, fakeColor, fakeRender)
}

func TestHighlight_Skill(t *testing.T) {
	h := newSkillHighlighter("plan", "review", "ponytail")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"found at start", "/plan fix", "<accent>/plan</> fix"},
		{"found after whitespace", "do /review now", "do <accent>/review</> now"},
		{"not found", "/unknown", "/unknown"},
		{"multiple known", "/plan /review", "<accent>/plan</> <accent>/review</>"},
		{"mixed known and unknown", "/plan /nope", "<accent>/plan</> /nope"},
		{"midword slash not matched", "path/plan", "path/plan"},
		{"bare slash not matched", "/ x", "/ x"},
		{"empty string", "", ""},
		{"plugin skill format", "/ponytail:ponytail fix", "<accent>/ponytail:ponytail</> fix"},
		{"plugin skill unknown name", "/ponytail:unknown-skill", "/ponytail:unknown-skill"},
		{"plugin skill unknown plugin", "/unknown:totally-unknown", "/unknown:totally-unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, h.Highlight(tt.in))
		})
	}
}

func TestHighlight_PartialTokenLeftUnstyled(t *testing.T) {
	h := newSkillHighlighter("plan")
	// Simulates a token split by the cursor: "/pl" is not a known skill.
	assert.Equal(t, "/pl", h.Highlight("/pl"))
}

func TestHighlight_AlreadyStyledNotReprocessed(t *testing.T) {
	h := newSkillHighlighter("plan")
	once := h.Highlight("/plan")
	assert.Equal(t, once, h.Highlight(once))
}

func TestHighlight_SkillBeforeShortcutSameSigil(t *testing.T) {
	rules := []Rule{
		SkillRule(knownSet("maintainer")),
		ShortcutRule(knownSet("git", "maintainer")),
	}
	h := New(rules, fakeColor, fakeRender)

	got := h.Highlight("/maintainer /git")
	assert.Equal(t, "<accent>/maintainer</> <status>/git</>", got)
}

func TestHighlight_OrderIndependentForDistinctSigils(t *testing.T) {
	skill := SkillRule(knownSet("plan"))
	file := FileRefRule(knownSet("main.go"))

	a := New([]Rule{skill, file}, fakeColor, fakeRender).Highlight("/plan @main.go")
	b := New([]Rule{file, skill}, fakeColor, fakeRender).Highlight("/plan @main.go")
	assert.Equal(t, a, b)
	assert.Equal(t, "<accent>/plan</> <success>@main.go</>", a)
}

func TestHighlight_FileRef(t *testing.T) {
	h := New([]Rule{FileRefRule(knownSet("main.go", "a/b.txt"))}, fakeColor, fakeRender)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"valid file", "see @main.go", "see <success>@main.go</>"},
		{"valid nested path", "@a/b.txt", "<success>@a/b.txt</>"},
		{"invalid file", "@nope.go", "@nope.go"},
		{"email-like not matched", "user@host", "user@host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, h.Highlight(tt.in))
		})
	}
}

func TestHighlight_FileRefFollowedByEscapeStillMatches(t *testing.T) {
	h := New([]Rule{FileRefRule(knownSet("main.go"))}, fakeColor, fakeRender)
	in := "@main.go\x1b[7m \x1b[0m"
	want := "<success>@main.go</>\x1b[7m \x1b[0m"
	assert.Equal(t, want, h.Highlight(in))
}

func TestHighlight_NilValidateAlwaysStyles(t *testing.T) {
	h := New([]Rule{IssueRefRule(nil)}, fakeColor, fakeRender)
	assert.Equal(t, "fixes <status>#365</>", h.Highlight("fixes #365"))
}

func TestHighlight_NoRulesIsNoOp(t *testing.T) {
	h := New(nil, fakeColor, fakeRender)
	assert.Equal(t, "/plan @main.go #1", h.Highlight("/plan @main.go #1"))
}

func TestHighlight_NilHighlighterSafe(t *testing.T) {
	var h *Highlighter
	assert.Equal(t, "/plan", h.Highlight("/plan"))
}

func TestRuleConstructors_Fields(t *testing.T) {
	skill := SkillRule(nil)
	assert.Equal(t, KindSkill, skill.Kind)
	assert.Equal(t, byte('/'), skill.Sigil)
	assert.Equal(t, "accent", skill.ColorKey)

	shortcut := ShortcutRule(nil)
	assert.Equal(t, KindShortcut, shortcut.Kind)
	assert.Equal(t, "status", shortcut.ColorKey)

	file := FileRefRule(nil)
	assert.Equal(t, KindFileRef, file.Kind)
	assert.Equal(t, byte('@'), file.Sigil)
	assert.Equal(t, "success", file.ColorKey)

	issue := IssueRefRule(nil)
	assert.Equal(t, KindIssueRef, issue.Kind)
	assert.Equal(t, byte('#'), issue.Sigil)
}
