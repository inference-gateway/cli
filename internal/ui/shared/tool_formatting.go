package shared

import (
	"fmt"
	"sort"
)

// FormatToolCall formats a tool call for consistent display across the application
func FormatToolCall(toolName string, args map[string]any) string {
	return FormatToolCallWithOptions(toolName, args, false)
}

// FormatToolCallWithOptions formats a tool call with options for expansion
func FormatToolCallWithOptions(toolName string, args map[string]any, expanded bool) string {
	if len(args) == 0 {
		return fmt.Sprintf("%s()", toolName)
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	argPairs := make([]string, 0, len(args))
	for _, key := range keys {
		value := args[key]
		var formattedValue string
		if !expanded && shouldCollapseArg(toolName, key) {
			formattedValue = `"..."`
		} else {
			formattedValue = fmt.Sprintf("%v", value)
		}
		argPairs = append(argPairs, fmt.Sprintf("%s=%s", key, formattedValue))
	}

	return fmt.Sprintf("%s(%s)", toolName, joinArgs(argPairs))
}

// shouldCollapseArg determines if a tool argument should be collapsed
// This is a fallback function - the proper way is through ToolFormatterService
// which delegates to individual tools' ShouldCollapseArg methods
func shouldCollapseArg(_, _ string) bool {
	return false
}

// joinArgs joins argument pairs with commas, handling long argument lists
func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) == 1 {
		return args[0]
	}

	result := args[0]
	for i := 1; i < len(args); i++ {
		result += ", " + args[i]
	}
	return result
}
