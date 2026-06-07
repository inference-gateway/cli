package config

import (
	"regexp"
	"strings"
)

// benignTrailingRedirectRe matches a single trailing shell redirection that only
// discards or merges output streams - it never writes to or reads from the
// filesystem. Covered forms: output to /dev/null (>, >>, with an optional fd or
// the &> "both streams" prefix) and file-descriptor duplications such as 2>&1 or
// 2>&-. These are stripped before allow-matching so that benign,
// reliability-motivated suffixes (which some models append by habit) neither
// break end-anchored patterns nor act as a policy escape hatch. A redirection to
// a real file is intentionally NOT matched, so it stays in the command.
var benignTrailingRedirectRe = regexp.MustCompile(
	`\s*(?:&>>?\s*/dev/null|[0-9]*>>?\s*/dev/null|[0-9]*>&(?:[0-9]+|-))\s*$`,
)

// bashAllowFor returns the effective bash allow-list for mode: the shared
// mode.all baseline unioned with that mode's own entries. An unrecognized mode
// (anything other than plan/standard/auto) gets just the baseline.
func (c *Config) bashAllowFor(mode string) []string {
	m := c.Tools.Bash.Mode
	out := make([]string, 0, len(m.All.Allow)+4)
	out = append(out, m.All.Allow...)
	switch mode {
	case "plan":
		out = append(out, m.Plan.Allow...)
	case "standard":
		out = append(out, m.Standard.Allow...)
	case "auto":
		out = append(out, m.Auto.Allow...)
	}
	return out
}

// BashAllowedCommands returns the effective allow-list entries for mode. It is
// used to surface the model's bash sandbox in the system prompt so the agent
// knows up front what it may run unattended.
func (c *Config) BashAllowedCommands(mode string) []string {
	return c.bashAllowFor(mode)
}

// IsBashCommandAllowed reports whether command is auto-approved in the given
// agent mode ("all", "plan", "standard", or "auto"). The model is a pure
// allow-list with no separate deny list: anything the effective list does not
// match is denied - in chat mode it falls through to user approval, in headless
// agent mode it is rejected with a reason (see BashCommandRejectionHint).
//
//   - If the effective allow-list contains the sentinel ".*" the mode is
//     UNRESTRICTED: any single command is allowed and the clean-command guard is
//     skipped (full autonomy - this is what mode.auto / headless `infer agent`
//     uses). Tighten mode.auto.allow to a curated list to re-enable the guard.
//   - Otherwise the clean-command guard runs first and rejects, regardless of the
//     list: command substitution ($(...), backticks, <()/>()), multi-command
//     chains/pipelines (top-level &&, ||, |, ;, &, newline), a surviving
//     file-write redirect (>, >>), dangerous find actions (-exec/-delete/...),
//     and printing/publishing an expanded $VAR (echo/printf/gh ... $SECRET,
//     which would leak the value).
//   - The single clean command is then matched WHOLE against each allow entry
//     (anchored as \A(?:entry)\z), so a bare "gh" allows only "gh" - an entry
//     must opt into arguments explicitly (e.g. "gh issue.*").
//
// It is the single source of truth consulted by the Bash tool, the approval
// policy, and agent auto-approval, so all three agree on exactly what runs
// without prompting.
func (c *Config) IsBashCommandAllowed(command, mode string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	allow := c.bashAllowFor(mode)
	if hasAllowAll(allow) {
		return true
	}

	seg, ok := cleanSingleCommand(command)
	if !ok {
		return false
	}

	return matchesAnyAllow(seg, allow)
}

// BashCommandRejectionHint returns a short, actionable explanation when command
// is rejected by the clean-command guard (substitution, a compound/piped command,
// an env-var leak, a file-write redirect, or a dangerous find action), so the
// model gets precise feedback it can act on rather than a bare "not allowed". It
// returns "" when the command is simply not in the allow-list with no structural
// reason - the bare message ("command not allowed: <cmd>") already tells the
// model to try a different command. Callers surface it only alongside an actual
// rejection.
func BashCommandRejectionHint(command string) string {
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
		return "the command has unbalanced quotes; fix the quoting and try again"
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
		return "output redirection to a file ('>' or '>>') is not auto-approved " +
			"(benign forms like '2>&1' or '>/dev/null' are allowed)"
	}
	if isDangerousFind(seg) {
		return "find actions that execute or mutate (-exec, -delete, ...) are not auto-approved; " +
			"use find for read-only discovery only"
	}

	return ""
}

// cleanSingleCommand validates that command is a single, side-effect-free shell
// command suitable for whole-command allow-matching, returning the normalized
// segment (benign trailing redirections stripped). ok is false - the command is
// not auto-approvable regardless of the allow-list - when it uses command
// substitution, chains/pipes more than one command, has unbalanced quoting,
// writes to a file, runs a dangerous find action, or prints/publishes an
// expanded environment variable.
func cleanSingleCommand(command string) (seg string, ok bool) {
	if containsCommandSubstitution(command) {
		return "", false
	}

	segments, balanced := splitBashSegments(command)
	if !balanced || len(segments) != 1 {
		return "", false
	}

	seg = stripBenignTrailingRedirections(strings.TrimSpace(segments[0]))
	if seg == "" {
		return "", false
	}

	if outputCommandRe.MatchString(seg) && containsVariableExpansion(seg) {
		return "", false
	}
	if containsFileRedirect(seg) {
		return "", false
	}
	if isDangerousFind(seg) {
		return "", false
	}

	return seg, true
}

// hasAllowAll reports whether the allow-list contains the "allow any command"
// sentinel, which makes the mode unrestricted (and skips the clean-command
// guard). ".*", "^.*$", ".+", and a few trivially-equivalent forms qualify.
func hasAllowAll(allow []string) bool {
	for _, entry := range allow {
		switch strings.TrimSpace(entry) {
		case ".*", "^.*$", "^.*", ".*$", ".+", "^.+$", "^.+", ".+$":
			return true
		}
	}
	return false
}

// matchesAnyAllow reports whether seg matches any allow entry as a WHOLE command.
// Each entry is wrapped as \A(?:entry)\z so it must match the entire command - a
// prefix match is never enough, and any anchors the entry already carries are
// harmless. Invalid regexes are skipped rather than failing the whole check.
func matchesAnyAllow(seg string, allow []string) bool {
	for _, entry := range allow {
		if entry == "" {
			continue
		}
		matched, err := regexp.MatchString(`\A(?:`+entry+`)\z`, seg)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// isDangerousFind reports whether seg is a find(1) invocation carrying a primary
// that executes a command or mutates the filesystem (-exec, -delete, ...). A
// bare "find" is fine for read-only discovery, but these actions turn it into an
// arbitrary-command / delete vector.
func isDangerousFind(seg string) bool {
	return (seg == "find" || strings.HasPrefix(seg, "find ")) &&
		dangerousFindActionRe.MatchString(seg)
}

// dangerousFindActionRe matches find(1) primaries that execute a command or
// mutate the filesystem (-exec, -delete, ...).
var dangerousFindActionRe = regexp.MustCompile(
	`(^|\s)-(execdir|exec|okdir|ok|delete|fprintf|fprint0|fprint|fls)(\s|$)`,
)

// outputCommandRe matches a command whose effect is to emit its arguments where a
// secret would become visible: echo/printf write to stdout (which the model
// reads back), and the gh issue/pr subcommands publish to GitHub. Variable
// expansion is allowed in general, but expanding one in such a command (echo
// $TOKEN, gh issue comment --body $TOKEN) would leak the value, so a match here
// combined with containsVariableExpansion blocks auto-approval. It is evaluated
// on the single command segment (after benign redirections are stripped), so it
// sees the real command name.
var outputCommandRe = regexp.MustCompile(
	`^(echo|printf)( |$)|^gh (issue|pr) (create|comment|edit)( |$)`,
)

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
// unbalanced, which the caller treats as not-allowed.
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
