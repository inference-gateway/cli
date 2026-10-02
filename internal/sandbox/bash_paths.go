package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	config "github.com/inference-gateway/cli/config"
)

// shellWord is one word of a command after quote and variable expansion, with
// what bash still applies to it: a leading unquoted tilde, unquoted glob
// characters, and expansions whose result cannot be known in advance.
type shellWord struct {
	text    string
	tilde   bool
	glob    bool
	dynamic bool
}

// printOnlyCommands never open their arguments as files.
var printOnlyCommands = []string{"echo", "printf"}

// creatingCommands create the paths they are given, so those are checked as writes.
var creatingCommands = []string{"mkdir", "ln"}

// bashPathOutsideSandbox reports the first path an allow-listed command would
// read or create outside the sandbox or on a protected path. A path the check
// cannot predict counts as outside.
func bashPathOutsideSandbox(cfg *config.Config, seg string) (string, bool) {
	words := splitShellWords(seg)
	if len(words) < 2 || slices.Contains(printOnlyCommands, words[0].text) {
		return "", false
	}

	validate := func(path string) error { return ValidateRead(cfg, path) }
	if slices.Contains(creatingCommands, words[0].text) {
		validate = func(path string) error { return ValidateWrite(cfg, path) }
	}
	args := words[1:]
	if words[0].text == "ln" {
		args = args[len(args)-1:]
	}

	for _, arg := range args {
		for _, candidate := range pathCandidates(arg) {
			if path, outside := wordOutsideSandbox(cfg, candidate, validate); outside {
				return path, true
			}
		}
	}
	return "", false
}

// pathCandidates returns the parts of an argument that may name a file: the
// word itself, the value of a --flag=value, or the value glued to a short -Xvalue.
func pathCandidates(arg shellWord) []shellWord {
	switch {
	case strings.HasPrefix(arg.text, "--"):
		_, value, found := strings.Cut(arg.text, "=")
		if !found || value == "" {
			return nil
		}
		return []shellWord{{text: value, glob: arg.glob, dynamic: arg.dynamic}}
	case strings.HasPrefix(arg.text, "-"):
		if len(arg.text) <= 2 {
			return nil
		}
		return []shellWord{{text: arg.text[2:], glob: arg.glob, dynamic: arg.dynamic}}
	default:
		return []shellWord{arg}
	}
}

func wordOutsideSandbox(cfg *config.Config, word shellWord, validate func(string) error) (string, bool) {
	if word.dynamic {
		return word.text, true
	}
	path := word.text
	if word.tilde {
		expanded, ok := expandTilde(path)
		if !ok {
			return path, true
		}
		path = expanded
	}
	if word.glob {
		return globOutsideSandbox(path, validate)
	}
	if validate(path) != nil {
		return path, true
	}
	return "", false
}

// expandTilde expands ~ and ~/path to the home directory. Other forms (~user,
// ~+, ~-) resolve to directories the check cannot predict, so ok is false.
func expandTilde(path string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	if path == "~" {
		return home, true
	}
	if rest, found := strings.CutPrefix(path, "~/"); found {
		return filepath.Join(home, rest), true
	}
	return "", false
}

// globOutsideSandbox checks the pattern and every file it expands to. A glob
// component starting with a dot can expand to ".." on older bash, so it is
// never auto-approved.
func globOutsideSandbox(pattern string, validate func(string) error) (string, bool) {
	patternParts := pathComponents(pattern)
	for _, part := range patternParts {
		if strings.HasPrefix(part, ".") && hasGlobChar(part) {
			return pattern, true
		}
	}
	if validate(pattern) != nil {
		return pattern, true
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return pattern, true
	}
	for _, match := range matches {
		if bashSkipsHiddenMatch(patternParts, pathComponents(match)) {
			continue
		}
		if validate(match) != nil {
			return match, true
		}
	}
	return "", false
}

// bashSkipsHiddenMatch reports whether bash would leave out a match that Go's
// glob includes: a hidden entry matched by a wildcard that does not start with a dot.
func bashSkipsHiddenMatch(patternParts, matchParts []string) bool {
	if len(patternParts) != len(matchParts) {
		return false
	}
	for i, part := range patternParts {
		if hasGlobChar(part) && strings.HasPrefix(matchParts[i], ".") {
			return true
		}
	}
	return false
}

func pathComponents(path string) []string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	return strings.Split(abs, string(filepath.Separator))
}

func hasGlobChar(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

// writesFileByOption reports whether an otherwise read-only command writes a
// file: sort -o, tree -o, git --output, or uniq's output operand. Long options
// match any prefix of --output, since getopt accepts abbreviations.
func writesFileByOption(words []shellWord) bool {
	if len(words) == 0 {
		return false
	}
	args := make([]string, 0, len(words)-1)
	for _, w := range words[1:] {
		args = append(args, w.text)
	}
	switch words[0].text {
	case "sort", "tree":
		return slices.ContainsFunc(args, func(a string) bool { return isOutputOption(a) || hasShortOption(a, 'o') })
	case "git":
		return slices.ContainsFunc(args, isOutputOption)
	case "uniq":
		operands := slices.DeleteFunc(args, func(a string) bool { return strings.HasPrefix(a, "-") })
		return len(operands) > 1
	}
	return false
}

func isOutputOption(arg string) bool {
	name, _, _ := strings.Cut(arg, "=")
	return len(name) > 2 && strings.HasPrefix("--output", name)
}

func hasShortOption(arg string, option rune) bool {
	return strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg[1:], option)
}

// splitShellWords splits a single clean command into words the way bash does:
// at unquoted blanks and input redirections, removing quotes and escapes.
func splitShellWords(seg string) []shellWord { //nolint:gocyclo,cyclop
	var words []shellWord
	var cur strings.Builder
	var word shellWord
	var inSingle, inDouble, started, brace bool
	runes := []rune(seg)

	flush := func() {
		if started {
			word.text = cur.String()
			word.dynamic = word.dynamic || brace && isBraceExpansion(word.text)
			words = append(words, word)
		}
		cur.Reset()
		word = shellWord{}
		started, brace = false, false
	}

	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch {
		case inSingle:
			if ch == '\'' {
				inSingle = false
				continue
			}
			cur.WriteRune(ch)
		case inDouble:
			switch {
			case ch == '"':
				inDouble = false
			case ch == '\\' && i+1 < len(runes) && strings.ContainsRune("$`\"\\", runes[i+1]):
				i++
				cur.WriteRune(runes[i])
			case ch == '$' && isVariableExpansionStart(runes, i):
				i = writeVariable(&cur, &word, runes, i, true)
			default:
				cur.WriteRune(ch)
			}
		default:
			switch {
			case ch == ' ' || ch == '\t' || ch == '<':
				flush()
				continue
			case ch == '\'':
				inSingle = true
			case ch == '"':
				inDouble = true
			case ch == '\\':
				if i+1 < len(runes) {
					i++
					cur.WriteRune(runes[i])
				}
			case ch == '~' && !started:
				word.tilde = true
				cur.WriteRune(ch)
			case ch == '$' && isVariableExpansionStart(runes, i):
				i = writeVariable(&cur, &word, runes, i, false)
			case ch == '{':
				brace = true
				cur.WriteRune(ch)
			case strings.ContainsRune("*?[", ch):
				word.glob = true
				cur.WriteRune(ch)
			default:
				cur.WriteRune(ch)
			}
		}
		started = true
	}
	flush()
	return words
}

// writeVariable writes the value bash gives the $NAME or ${NAME} at runes[i],
// read from the environment the Bash tool hands to bash, and returns the index
// of its last rune. A form the check cannot predict marks the word dynamic.
func writeVariable(cur *strings.Builder, word *shellWord, runes []rune, i int, quoted bool) int {
	value, end, ok := expandVariable(runes, i, quoted)
	if !ok {
		word.dynamic = true
		cur.WriteRune('$')
		return i
	}
	cur.WriteString(value)
	return end
}

// expandVariable expands the $NAME or ${NAME} at runes[i]. ok is false for
// special parameters, ${...} operators, and an unquoted value bash would split
// into more words or expand as a glob.
func expandVariable(runes []rune, i int, quoted bool) (value string, end int, ok bool) {
	start := i + 1
	braced := runes[start] == '{'
	if braced {
		start++
	}
	end = start
	for end < len(runes) && isNameRune(runes[end]) {
		end++
	}
	if end == start || unicode.IsDigit(runes[start]) {
		return "", i, false
	}
	name := string(runes[start:end])
	if braced {
		if end >= len(runes) || runes[end] != '}' {
			return "", i, false
		}
		end++
	}
	value = os.Getenv(name)
	if !quoted && strings.ContainsAny(value, " \t\n*?[") {
		return "", i, false
	}
	return value, end - 1, true
}

// isBraceExpansion reports whether a word with an unquoted brace expands into
// several words, as {a,b} and {1..3} do. A brace without either, such as gh's
// {owner} placeholder, stays literal.
func isBraceExpansion(text string) bool {
	return strings.Contains(text, ",") || strings.Contains(text, "..")
}
