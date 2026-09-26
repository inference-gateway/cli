package inputsyntax

import (
	"regexp"
	"strings"
)

// Kind identifies a class of reserved input token.
type Kind int

const (
	// KindSkill is a "/<name>" token that routes to an installed agent skill.
	KindSkill Kind = iota
	// KindCatalogSkill is a "/<name>" token naming a catalog skill that is not
	// installed locally yet - invoking it downloads the skill first.
	KindCatalogSkill
	// KindShortcut is a "/<name>" token that maps to a chat shortcut command.
	KindShortcut
	// KindFileRef is an "@<path>" token expanded into file content.
	KindFileRef
	// KindIssueRef is a "#<number>" token (reserved for future use).
	KindIssueRef
)

// Rule describes one token class: how to find it, whether a given body is a real
// reference, and which theme color key to paint it with.
//
// Regex contract: exactly two capture groups.
//
//	group 1 = the leading boundary (start-of-string or a single whitespace rune),
//	          re-emitted UNSTYLED (Go's regexp has no lookbehind).
//	group 2 = the full token INCLUDING its sigil (e.g. "/plan", "@a/b", "#365").
//
// Sigil must be the first byte of group 2.
type Rule struct {
	Kind     Kind
	Re       *regexp.Regexp
	Sigil    byte
	ColorKey string
	Validate func(name string) bool
}

// Highlighter applies an ordered set of rules to an already-rendered string.
type Highlighter struct {
	rules   []Rule
	colorOf func(key string) string
	render  func(text, hexColor string) string
}

// New builds a Highlighter. colorOf resolves a theme color key to a hex color
// and render styles text with that color (e.g. styleProvider.GetThemeColor and
// styleProvider.RenderWithColor). rules may be empty, making Highlight a no-op.
func New(rules []Rule, colorOf func(key string) string, render func(text, hexColor string) string) *Highlighter {
	return &Highlighter{rules: rules, colorOf: colorOf, render: render}
}

// Highlight runs every rule over s and returns the styled string. Rules are
// applied in order; since each rule already styles only genuine plain-text
// tokens (an earlier rule's ANSI moves a token's sigil off the boundary), the
// ordering matters only for tokens sharing a sigil (skills before shortcuts).
func (h *Highlighter) Highlight(s string) string {
	if h == nil || h.colorOf == nil || h.render == nil {
		return s
	}
	for i := range h.rules {
		r := h.rules[i]
		color := h.colorOf(r.ColorKey)
		s = r.Re.ReplaceAllStringFunc(s, func(match string) string {
			idx := strings.IndexByte(match, r.Sigil)
			if idx < 0 {
				return match
			}
			lead, token := match[:idx], match[idx:]
			if r.Validate != nil && !r.Validate(token[1:]) {
				return match
			}
			return lead + h.render(token, color)
		})
	}
	return s
}
