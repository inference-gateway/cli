package sandbox

import (
	"fmt"
	"regexp"
	"strings"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// benignTrailingRedirectRe matches one trailing redirection that only discards
// or merges streams, such as >/dev/null or 2>&1. It is stripped before
// allow-matching so habitual suffixes neither break end-anchored patterns nor
// escape the policy. A redirection to a real file never matches.
var benignTrailingRedirectRe = regexp.MustCompile(
	`\s*(?:&>>?\s*/dev/null|[0-9]*>>?\s*/dev/null|[0-9]*>&(?:[0-9]+|-))\s*$`,
)

// BashAllowedCommands returns the effective bash allow-list for mode: the shared
// mode.all baseline unioned with that mode's own bucket. This is the ONE place
// an AgentMode maps to a tools.bash.mode.<bucket> list: Plan -> plan,
// AutoAccept -> auto, Standard/AutoWithJudge/unknown -> standard, ReadOnly ->
// baseline only. The system prompt lists it so the agent knows what runs.
func BashAllowedCommands(cfg *config.Config, mode agentdomain.AgentMode) []string {
	m := cfg.Tools.Bash.Mode
	out := make([]string, 0, len(m.All.Allow)+4)
	out = append(out, m.All.Allow...)
	switch mode {
	case agentdomain.AgentModePlan:
		out = append(out, m.Plan.Allow...)
	case agentdomain.AgentModeAutoAccept:
		out = append(out, m.Auto.Allow...)
	case agentdomain.AgentModeReadOnly:
	default:
		out = append(out, m.Standard.Allow...)
	}
	return out
}

// IsBashCommandAllowed reports whether command is auto-approved in mode, for the
// Bash tool, the approval policy and agent auto-approval alike. A ".*" entry
// allows anything. Otherwise the command must pass cleanSingleCommand, match an
// allow entry whole, and name only paths inside the sandbox. What is not
// allowed asks in chat and is rejected in headless mode.
func IsBashCommandAllowed(cfg *config.Config, command string, mode agentdomain.AgentMode) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}

	allow := BashAllowedCommands(cfg, mode)
	if IsUnrestricted(allow) {
		return true
	}

	seg, ok := cleanSingleCommand(command)
	if !ok || !matchesAnyAllow(seg, allow) {
		return false
	}

	_, outside := bashPathOutsideSandbox(cfg, seg, mode)
	return !outside
}

// BashCommandRejectionHint explains why the guard rejected command, or a path
// outside the sandbox, so the model can correct course instead of retrying.
// It returns "" when the command is simply not in the allow-list. A nil config
// skips the sandbox reason.
func BashCommandRejectionHint(cfg *config.Config, command string) string {
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
	if writesFileByOption(splitShellWords(seg)) {
		return "writing a file through an option (sort -o, tree -o, git --output, uniq's output " +
			"operand) is not auto-approved; print the output instead"
	}
	if cfg == nil {
		return ""
	}
	if path, outside := bashPathOutsideSandbox(cfg, seg, agentdomain.AgentModeStandard); outside {
		return fmt.Sprintf("'%s' is outside the sandbox, protected, or depends on the environment, "+
			"so the command needs approval; use paths inside the sandbox, or tell the user what you need", path)
	}

	return ""
}

// cleanSingleCommand returns command as one side-effect-free segment with
// benign redirections stripped. ok is false for command substitution, chains
// and pipes, unbalanced quotes, file writes, dangerous find actions, or a
// printed or published variable expansion.
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
	if writesFileByOption(splitShellWords(seg)) {
		return "", false
	}

	return seg, true
}

// IsUnrestricted reports whether the allow-list contains the "allow any command"
// sentinel, which makes the mode unrestricted (and skips the clean-command
// guard). ".*", "^.*$", ".+", and a few trivially-equivalent forms qualify.
func IsUnrestricted(allow []string) bool {
	for _, entry := range allow {
		switch strings.TrimSpace(entry) {
		case ".*", "^.*$", "^.*", ".*$", ".+", "^.+$", "^.+", ".+$":
			return true
		}
	}
	return false
}

// matchesAnyAllow reports whether seg matches any allow entry as a whole
// command, wrapping each as \A(?s:entry)\z. The s flag lets . span quoted
// newlines in a multi-line commit message. Invalid regexes are skipped.
func matchesAnyAllow(seg string, allow []string) bool {
	for _, entry := range allow {
		if entry == "" {
			continue
		}
		matched, err := regexp.MatchString(`\A(?s:`+entry+`)\z`, seg)
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

// outputCommandRe matches commands that expose their arguments: echo and printf
// print them back to the model and gh issue and pr publish them. Combined with
// containsVariableExpansion it blocks auto-approval, since echo $TOKEN would
// leak the value. It runs on the redirect-stripped segment.
var outputCommandRe = regexp.MustCompile(
	`^(echo|printf)( |$)|^gh (issue|pr) (create|comment|edit)( |$)`,
)

// containsCommandSubstitution reports whether command runs a nested command
// through $(...), backticks or <(...) and >(...). Single-quoted spans are
// ignored. $(...) and backticks count inside double quotes, while process
// substitution counts only unquoted.
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

// containsVariableExpansion reports whether command expands a variable such as
// $NAME, ${...} or $1 outside single quotes, which could leak a secret from an
// otherwise read-only command. Escaped '$' is ignored, and '$(' is left to
// containsCommandSubstitution.
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

// consumeAmpersand classifies the unquoted '&' at i and returns how many extra
// runes to skip. "&&" splits and skips the second '&', "&>" and the '&' of a
// ">&" duplication stay in the segment, and a lone '&' splits as the
// background operator.
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
