package config

import (
	"regexp"
	"strings"
)

// benignTrailingRedirectRe matches a single trailing shell redirection that only
// discards or merges output streams - it never writes to or reads from the
// filesystem. Covered forms: output to /dev/null (>, >>, with an optional fd or
// the &> "both streams" prefix) and file-descriptor duplications such as 2>&1 or
// 2>&-. These are stripped before whitelist matching so that benign,
// reliability-motivated suffixes (which some models append by habit) neither
// break end-anchored patterns nor act as a policy escape hatch. A redirection to
// a real file is intentionally NOT matched, so it stays in the command.
var benignTrailingRedirectRe = regexp.MustCompile(
	`\s*(?:&>>?\s*/dev/null|[0-9]*>>?\s*/dev/null|[0-9]*>&(?:[0-9]+|-))\s*$`,
)

// IsBashCommandWhitelisted reports whether command is permitted by the bash
// whitelist. It understands just enough shell structure to keep the policy
// coherent rather than relying on naive prefix matching:
//
//   - Command substitution - $(...), `...`, and process substitution <(...) /
//     >(...) - is rejected outright, because it can smuggle an arbitrary command
//     past an otherwise-whitelisted wrapper (e.g. echo $(rm -rf /)).
//   - Shell variable/parameter expansion ($VAR, ${VAR}) may be USED freely, but a
//     command that prints (echo, printf) or publishes (gh issue/pr
//     create|comment|edit) its arguments may not expand one - that would leak a
//     secret's value (echo $AWS_SECRET_ACCESS_KEY). A single-quoted or
//     backslash-escaped '$' is always a literal and stays allowed.
//   - Only a SINGLE command is auto-whitelisted. Any top-level shell operator
//     (&&, ||, |, |&, ;, &, or a newline) makes the whole command non-whitelisted
//     so it falls through to approval. This keeps the policy simple and closes the
//     prefix-match hole where a whitelisted head would carry an arbitrary tail
//     (echo x | xargs rm); chains and pipelines are run one command at a time.
//   - Benign trailing redirections (2>&1, 2>/dev/null, …) are stripped per
//     segment before matching.
//   - A file-write redirection that survives stripping (>, >>, &>file) restricts
//     its segment to whole-command pattern matching: the plain command list no
//     longer applies and a prefix pattern (^git log) will not unlock it, so a
//     whitelisted command cannot be turned into an arbitrary file write
//     (echo secret > /etc/passwd). An anchored pattern (^…$) can still allow a
//     specific redirect.
//
// It is the single source of truth consulted by the Bash tool, the approval
// policy, and agent auto-approval, so all three agree on exactly what runs
// without prompting.
func (c *Config) IsBashCommandWhitelisted(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	if containsCommandSubstitution(command) {
		return false
	}

	segments, ok := splitBashSegments(command)
	if !ok {
		return false
	}

	if len(segments) != 1 {
		return false
	}

	seg := stripBenignTrailingRedirections(strings.TrimSpace(segments[0]))
	if seg == "" {
		return false
	}

	// Env-var leak guard: $VAR may be used freely, but a command that prints
	// (echo/printf) or publishes (gh issue/pr create|comment|edit) its arguments
	// must not expand one, or it would leak the value (echo $AWS_SECRET_ACCESS_KEY).
	if outputCommandRe.MatchString(seg) && containsVariableExpansion(seg) {
		return false
	}

	return c.isSingleBashCommandAllowed(seg)
}

// BashWhitelistRejectionHint returns a short, actionable explanation when
// command uses a restricted shell construct - command substitution, a
// compound/piped command, an env-var leak (printing/publishing $VAR), or a
// file-write redirection - so the model gets precise feedback instead of a bare
// "not whitelisted". It returns "" when no special explanation applies; callers
// should surface it only alongside an actual rejection.
func BashWhitelistRejectionHint(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}

	if containsCommandSubstitution(command) {
		return "command substitution ($(...), backticks, <(...), >(...)) is not permitted; " +
			"run the inner command directly instead"
	}

	segments, ok := splitBashSegments(command)
	if !ok {
		return ""
	}
	if len(segments) != 1 {
		return "only a single command is auto-approved; pipes and operators (|, &&, ||, ;, &) " +
			"are not - run one command at a time instead of chaining them"
	}

	seg := stripBenignTrailingRedirections(strings.TrimSpace(segments[0]))
	if outputCommandRe.MatchString(seg) && containsVariableExpansion(seg) {
		return "a printing or publishing command (echo, printf, gh issue/pr create|comment|edit) " +
			"may not expand an environment variable ($VAR) - it would leak the value; use single " +
			"quotes for a literal '$' (echo '$HOME') or omit the variable"
	}
	if containsFileRedirect(seg) {
		return "output redirection to a file ('>' or '>>') is restricted by default " +
			"(benign forms like '2>&1' or '>/dev/null' are allowed); to permit this exact " +
			"command, add an anchored regex (^...$) to tools.bash.whitelist.commands"
	}
	return ""
}

// dangerousFindActionRe matches find(1) primaries that execute a command or
// mutate the filesystem (-exec, -delete, …). A bare "find" is whitelisted for
// read-only discovery, but these actions turn it into an arbitrary-command /
// delete vector, so a find invocation carrying one is not whitelisted (it falls
// through to approval) - the same stance taken on command substitution.
var dangerousFindActionRe = regexp.MustCompile(
	`(^|\s)-(execdir|exec|okdir|ok|delete|fprintf|fprint0|fprint|fls)(\s|$)`,
)

// outputCommandRe matches a command whose effect is to emit its arguments where a
// secret would become visible: echo/printf write to stdout (which the model
// reads back), and the gh issue/pr subcommands publish to GitHub. Variable
// expansion is allowed in general, but expanding one in such a command (echo
// $TOKEN, gh issue comment --body $TOKEN) would leak the value, so a match here
// combined with containsVariableExpansion blocks auto-whitelisting. It is
// evaluated on the single command segment (after benign redirections are
// stripped), so it sees the real command name.
var outputCommandRe = regexp.MustCompile(
	`^(echo|printf)( |$)|^gh (issue|pr) (create|comment|edit)( |$)`,
)

// isBashEntryRegex reports whether entry should be treated as a regex (rather
// than a bare-token exact match). A bare token must match the command exactly
// (e.g. "gh" allows only "gh", never "gh issue list"). The classifier errs on
// the side of calling it a regex when the entry contains a space or any standard
// regex metacharacter (^ $ * + ? ( ) [ ] { } | \). Lone '.' and '-' stay bare
// so that e.g. "python3.11" and "git-lfs" remain exact matches.
func isBashEntryRegex(entry string) bool {
	if strings.Contains(entry, " ") {
		return true
	}
	return strings.ContainsAny(entry, "^$*+?()[]{}|\\")
}

// isSingleBashCommandAllowed matches one already-split segment (with benign
// trailing redirections already stripped) against the unified whitelist.
// Each whitelist entry is classified as a bare token (exact match) or a regex
// via isBashEntryRegex; regex entries are matched with regexp.MatchString.
//
// A segment that still carries a file-write redirection (>, >>) bypasses bare-
// token matching entirely and is allowed only by a regex that matches the
// entire command (via matchesEntirePattern), so a whitelisted command cannot
// smuggle in an arbitrary write target.
func (c *Config) isSingleBashCommandAllowed(command string) bool {
	if (command == "find" || strings.HasPrefix(command, "find ")) &&
		dangerousFindActionRe.MatchString(command) {
		return false
	}

	hasFileRedirect := containsFileRedirect(command)

	for _, entry := range c.Tools.Bash.Whitelist.Commands {
		if isBashEntryRegex(entry) && hasFileRedirect {
			if matchesEntirePattern(entry, command) {
				return true
			}
			continue
		}

		if isBashEntryRegex(entry) {
			matched, err := regexp.MatchString(entry, command)
			if err == nil && matched {
				return true
			}
		} else if !hasFileRedirect {
			if command == entry {
				return true
			}
		}
	}

	return false
}

// matchesEntirePattern reports whether pattern matches command in its entirety,
// regardless of whether pattern carries its own anchors. It is used for segments
// that contain a file-write redirection, where a mere prefix match (e.g. ^git
// log against "git log > /etc/passwd") must not be enough to unlock the command.
func matchesEntirePattern(pattern, command string) bool {
	matched, err := regexp.MatchString(`\A(?:`+pattern+`)\z`, command)
	return err == nil && matched
}

// stripBenignTrailingRedirections removes any run of trailing benign
// redirections (see benignTrailingRedirectRe) from command, e.g. turning
// "gh api repos/o/r > /dev/null 2>&1" into "gh api repos/o/r".
func stripBenignTrailingRedirections(command string) string {
	for {
		loc := benignTrailingRedirectRe.FindStringIndex(command)
		if loc == nil {
			break
		}
		command = command[:loc[0]]
	}
	return strings.TrimSpace(command)
}

// containsCommandSubstitution reports whether command contains a shell construct
// that executes a nested command: $(...), backticks, or process substitution
// <(...) / >(...). Single-quoted spans are literal in bash, so constructs inside
// them are ignored. $(...) and backticks are still active inside double quotes,
// so those are flagged there; process substitution does not occur inside double
// quotes, so <(/>( are flagged only when fully unquoted.
func containsCommandSubstitution(command string) bool {
	var inSingle, inDouble bool
	runes := []rune(command)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch {
		case inSingle:
			if ch == '\'' {
				inSingle = false
			}
		case inDouble:
			switch ch {
			case '\\':
				i++
			case '"':
				inDouble = false
			case '`':
				return true
			case '$':
				if i+1 < len(runes) && runes[i+1] == '(' {
					return true
				}
			}
		default:
			switch ch {
			case '\\':
				i++
			case '\'':
				inSingle = true
			case '"':
				inDouble = true
			case '`':
				return true
			case '$':
				if i+1 < len(runes) && runes[i+1] == '(' {
					return true
				}
			case '<', '>':
				if i+1 < len(runes) && runes[i+1] == '(' {
					return true
				}
			}
		}
	}

	return false
}

// containsVariableExpansion reports whether command performs shell parameter or
// variable expansion - $NAME, ${...}, or a special parameter such as $1/$?/$@ -
// outside single quotes. Single-quoted spans are literal in bash, so a '$' there
// is ignored (echo '$HOME' prints the text verbatim and is safe). Inside double
// quotes and when unquoted, '$NAME' expands, which would let an otherwise
// read-only command leak environment variables (echo $AWS_SECRET_ACCESS_KEY), so
// it is flagged. A backslash-escaped '$' is literal and ignored. Command
// substitution ('$(') is handled by containsCommandSubstitution, so it is not
// re-flagged here.
func containsVariableExpansion(command string) bool {
	var inSingle, inDouble bool
	runes := []rune(command)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch {
		case inSingle:
			if ch == '\'' {
				inSingle = false
			}
		case inDouble:
			switch ch {
			case '\\':
				i++
			case '"':
				inDouble = false
			case '$':
				if isVariableExpansionStart(runes, i) {
					return true
				}
			}
		default:
			switch ch {
			case '\\':
				i++
			case '\'':
				inSingle = true
			case '"':
				inDouble = true
			case '$':
				if isVariableExpansionStart(runes, i) {
					return true
				}
			}
		}
	}

	return false
}

// isVariableExpansionStart reports whether the '$' at runes[i] begins a parameter
// or variable expansion. It is true for "${...}" and for a '$' followed by a name
// character ([A-Za-z0-9_]) or a special parameter (@ * # ? ! $ -). A '$' at end
// of input, before whitespace/punctuation (a literal '$'), or before '(' (command
// substitution, handled elsewhere) returns false.
func isVariableExpansionStart(runes []rune, i int) bool {
	if i+1 >= len(runes) {
		return false
	}
	switch next := runes[i+1]; {
	case next == '(':
		return false
	case next == '{':
		return true
	case isNameRune(next):
		return true
	default:
		return strings.ContainsRune("@*#?!$-", next)
	}
}

// isNameRune reports whether r can appear in a shell variable name or is a single
// positional parameter ([A-Za-z0-9_]).
func isNameRune(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// containsFileRedirect reports whether seg contains an unquoted output
// redirection to a real file (>, >>, &>file, >&file). It is meant to run after
// stripBenignTrailingRedirections, so any surviving unquoted '>' denotes a write
// to the filesystem rather than a benign /dev/null or fd-duplication suffix. A
// '>' inside single or double quotes is a literal argument and is ignored.
func containsFileRedirect(seg string) bool {
	var inSingle, inDouble bool
	runes := []rune(seg)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch {
		case inSingle:
			if ch == '\'' {
				inSingle = false
			}
		case inDouble:
			switch ch {
			case '\\':
				i++
			case '"':
				inDouble = false
			}
		default:
			switch ch {
			case '\\':
				i++
			case '\'':
				inSingle = true
			case '"':
				inDouble = true
			case '>':
				return true
			}
		}
	}

	return false
}

// splitBashSegments splits command at top-level shell control operators (&&, ||,
// |, |&, ;, &, and newlines), honoring single quotes, double quotes, and
// backslash escapes. Redirection operators (>, <, >&, &>) are deliberately NOT
// split points and remain part of their segment. ok is false when quoting is
// unbalanced, which the caller treats as not-whitelisted.
func splitBashSegments(command string) (segments []string, ok bool) {
	var cur strings.Builder
	var inSingle, inDouble bool
	runes := []rune(command)

	flush := func() {
		segments = append(segments, cur.String())
		cur.Reset()
	}

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if inSingle {
			cur.WriteRune(ch)
			if ch == '\'' {
				inSingle = false
			}
			continue
		}
		if inDouble {
			if ch == '\\' && i+1 < len(runes) {
				cur.WriteRune(ch)
				i++
				cur.WriteRune(runes[i])
				continue
			}
			cur.WriteRune(ch)
			if ch == '"' {
				inDouble = false
			}
			continue
		}

		switch ch {
		case '\\':
			cur.WriteRune(ch)
			if i+1 < len(runes) {
				i++
				cur.WriteRune(runes[i])
			}
		case '\'':
			inSingle = true
			cur.WriteRune(ch)
		case '"':
			inDouble = true
			cur.WriteRune(ch)
		case ';', '\n', '\r':
			flush()
		case '&':
			i += consumeAmpersand(runes, i, &cur, flush)
		case '|':
			// "||" and "|&" are two-char control operators; "|" is a pipe. All split.
			if i+1 < len(runes) && (runes[i+1] == '|' || runes[i+1] == '&') {
				i++
			}
			flush()
		default:
			cur.WriteRune(ch)
		}
	}

	if inSingle || inDouble {
		return nil, false
	}
	flush()
	return segments, true
}

// consumeAmpersand classifies an unquoted '&' encountered at index i and returns
// how many extra runes to advance past (i is the '&' itself):
//
//   - "&&" → control operator: split, skip the second '&'.
//   - "&>" → redirection of both streams: keep the '&' in the current segment.
//   - the '&' completing a ">&" fd-duplication (e.g. the one in 2>&1): keep it.
//   - a lone trailing/standalone '&' → background operator: split.
func consumeAmpersand(runes []rune, i int, cur *strings.Builder, flush func()) int {
	if i+1 < len(runes) && runes[i+1] == '&' {
		flush()
		return 1
	}
	if i+1 < len(runes) && runes[i+1] == '>' {
		cur.WriteRune('&')
		return 0
	}
	if endsWithRedirectFD(cur.String()) {
		cur.WriteRune('&')
		return 0
	}
	flush()
	return 0
}

// endsWithRedirectFD reports whether s ends with a '>' (ignoring trailing
// blanks), meaning an immediately following '&' completes a file-descriptor
// duplication like "2>&1" rather than starting a background "&".
func endsWithRedirectFD(s string) bool {
	return strings.HasSuffix(strings.TrimRight(s, " \t"), ">")
}
