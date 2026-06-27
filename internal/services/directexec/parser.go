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
// satisfies the legacy domain.ChatHandler interface.
func (s *Service) ParseToolCall(input string) (string, map[string]any, error) {
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

	parsedArgs, err := s.ParseArguments(argsStr)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	return toolName, parsedArgs, nil
}

// ParseArguments parses function arguments in the format key="value",
// key2="value2". Numeric values are stored as float64; everything else as
// string.
func (s *Service) ParseArguments(argsStr string) (map[string]any, error) {
	args := make(map[string]any)

	if argsStr == "" {
		return args, nil
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
