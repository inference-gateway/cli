package directexec

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// argPattern matches `key="quoted value"`, `key='quoted value'`, or
// `key=bareword`. Compiled once at package load.
var argPattern = regexp.MustCompile(`(\w+)=("[^"]*"|'[^']*'|\w+)`)

// ParseToolCall parses a tool call in the format ToolName(arg="value",
// arg2="value2"). Exposed for testing and for use by the orchestrator that
// satisfies the legacy ui.ChatHandler interface.
func (s *Service) ParseToolCall(input string) (string, map[string]any, error) {
	return ParseToolCall(input)
}

// ParseArguments parses `key=value` pairs; see ParseToolCall.
func (s *Service) ParseArguments(argsStr string) (map[string]any, error) {
	return ParseArguments(argsStr)
}

// ParseToolCall is the package-level parser behind Service.ParseToolCall, so
// non-TUI front ends (headless) accept the same `!!Tool(arg="v")` syntax.
func ParseToolCall(input string) (string, map[string]any, error) {
	parenIndex := strings.Index(input, "(")
	if parenIndex == -1 {
		return "", nil, fmt.Errorf("missing opening parenthesis")
	}

	toolName := strings.TrimSpace(input[:parenIndex])
	if toolName == "" {
		return "", nil, fmt.Errorf("missing tool name")
	}

	argsStr := strings.TrimSpace(input[parenIndex+1:])
	if !strings.HasSuffix(argsStr, ")") {
		return "", nil, fmt.Errorf("missing closing parenthesis")
	}

	argsStr = strings.TrimSuffix(argsStr, ")")
	argsStr = strings.TrimSpace(argsStr)

	args := make(map[string]any)
	if argsStr == "" {
		return toolName, args, nil
	}

	if strings.HasPrefix(argsStr, "{") {
		if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
			return "", nil, fmt.Errorf("failed to parse JSON arguments: %w", err)
		}
		return toolName, args, nil
	}

	parsedArgs, err := ParseArguments(argsStr)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	return toolName, parsedArgs, nil
}

// ParseArguments parses function arguments in the format key="value",
// key2="value2". Numeric values are stored as float64; everything else as
// string.
func ParseArguments(argsStr string) (map[string]any, error) {
	args := make(map[string]any)

	if argsStr == "" {
		return args, nil
	}

	argsStr, err := extractJSONValues(argsStr, args)
	if err != nil {
		return nil, err
	}

	matches := argPattern.FindAllStringSubmatch(argsStr, -1)

	for _, match := range matches {
		if len(match) != 3 {
			continue
		}

		key := match[1]
		value := match[2]

		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
			(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			value = value[1 : len(value)-1]
		}

		if numValue, err := strconv.ParseFloat(value, 64); err == nil {
			args[key] = numValue
		} else {
			args[key] = value
		}
	}

	return args, nil
}

// extractJSONValues pulls `key=[...]` and `key={...}` pairs out of argsStr -
// values argPattern cannot express - decodes them into args, and returns the
// remainder for the regular key=value pass.
func extractJSONValues(argsStr string, args map[string]any) (string, error) {
	var rest strings.Builder
	i := 0
	for i < len(argsStr) {
		start, keyEnd, ok := jsonValueStart(argsStr, i)
		if !ok {
			rest.WriteString(argsStr[i:])
			break
		}
		end := matchingBracket(argsStr, keyEnd+1)
		if end < 0 {
			return "", fmt.Errorf("unterminated JSON value for %q", argsStr[start:keyEnd])
		}
		var value any
		if err := json.Unmarshal([]byte(argsStr[keyEnd+1:end+1]), &value); err != nil {
			return "", fmt.Errorf("invalid JSON value for %q: %w", argsStr[start:keyEnd], err)
		}
		args[argsStr[start:keyEnd]] = value
		rest.WriteString(argsStr[i:start])
		i = end + 1
	}
	return rest.String(), nil
}

// jsonValueStart finds the next `key=` followed by `[` or `{` at or after
// from, skipping over quoted strings so brackets inside them are ignored.
func jsonValueStart(s string, from int) (keyStart, keyEnd int, ok bool) {
	for i := from; i < len(s); i++ {
		switch s[i] {
		case '"', '\'':
			if j := strings.IndexByte(s[i+1:], s[i]); j >= 0 {
				i += j + 1
			}
		case '=':
			if i+1 < len(s) && (s[i+1] == '[' || s[i+1] == '{') {
				k := i
				for k > from && isWordByte(s[k-1]) {
					k--
				}
				if k < i {
					return k, i, true
				}
			}
		}
	}
	return 0, 0, false
}

// matchingBracket returns the index of the bracket closing the one at open,
// or -1. Brackets inside JSON strings do not count.
func matchingBracket(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
