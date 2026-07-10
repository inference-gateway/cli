package inputsyntax

import "regexp"

// slashRe matches a "/<name>" token at start-of-string or after whitespace. The
// [a-z0-9:-] charset mirrors the skill/shortcut name validation plus ':' for
// the /pluginName:skillName format. Skills and shortcuts share this pattern;
// they are distinguished by their Validate funcs and applied in order (skills
// first) so a known skill is never re-colored as a shortcut.
var slashRe = regexp.MustCompile(`(^|\s)(/[a-z0-9:-]+)`)

// fileRe matches an "@<path>" token at start-of-string or after whitespace,
// mirroring the file-reference grammar used during message expansion. The body
// excludes ESC (\x1b) so a path immediately followed by the cursor's ANSI markup
// (no trailing space) still matches cleanly instead of swallowing the escape.
var fileRe = regexp.MustCompile("(^|\\s)(@[^\\s\x1b]+)")

// issueRe matches a "#<number>" token at start-of-string or after whitespace.
// Reserved for a future GitHub-issue highlighting rule.
var issueRe = regexp.MustCompile(`(^|\s)(#[0-9]+)`)

// SkillRule highlights "/<skill>" tokens. validate resolves a lowercased skill
// name to a loaded skill (typically skillsService.Get).
func SkillRule(validate func(name string) bool) Rule {
	return Rule{Kind: KindSkill, Re: slashRe, Sigil: '/', ColorKey: "accent", Validate: validate}
}

// ShortcutRule highlights "/<shortcut>" tokens. validate resolves a shortcut
// name to a registered shortcut (typically registry.Get). Apply AFTER SkillRule.
func ShortcutRule(validate func(name string) bool) Rule {
	return Rule{Kind: KindShortcut, Re: slashRe, Sigil: '/', ColorKey: "status", Validate: validate}
}

// FileRefRule highlights "@<path>" tokens. validate reports whether the path is
// a real, accessible file (typically fileService.ValidateFile == nil).
func FileRefRule(validate func(name string) bool) Rule {
	return Rule{Kind: KindFileRef, Re: fileRe, Sigil: '@', ColorKey: "success", Validate: validate}
}

// IssueRefRule highlights "#<number>" tokens. validate may be nil to style any
// "#<digits>" token. Reserved for future use.
func IssueRefRule(validate func(name string) bool) Rule {
	return Rule{Kind: KindIssueRef, Re: issueRe, Sigil: '#', ColorKey: "status", Validate: validate}
}
