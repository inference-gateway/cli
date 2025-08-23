package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BaseFormatter provides common formatting functionality that tools can embed
type BaseFormatter struct {
	toolName string
}

// CustomFormatter extends BaseFormatter with customizable collapse behavior
type CustomFormatter struct {
	BaseFormatter
	collapseFunc func(string) bool
}

// NewBaseFormatter creates a new base formatter for a tool
func NewBaseFormatter(toolName string) BaseFormatter {
	return BaseFormatter{
		toolName: toolName,
	}
}

// NewCustomFormatter creates a formatter with custom collapse logic
func NewCustomFormatter(toolName string, collapseFunc func(string) bool) CustomFormatter {
	return CustomFormatter{
		BaseFormatter: NewBaseFormatter(toolName),
		collapseFunc:  collapseFunc,
	}
}

// FormatToolCall formats a tool call for consistent display
func (f BaseFormatter) FormatToolCall(args map[string]any, expanded bool) string {
	if len(args) == 0 {
		return fmt.Sprintf("%s()", f.toolName)
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	argPairs := make([]string, 0, len(args))
	for _, key := range keys {
		value := args[key]
		if !expanded && f.ShouldCollapseArg(key) {
			value = f.collapseArgValue(value, 50)
		}
		argPairs = append(argPairs, fmt.Sprintf("%s=%v", key, value))
	}

	return fmt.Sprintf("%s(%s)", f.toolName, f.joinArgs(argPairs))
}

// FormatStatus returns a formatted status with icon
func (f BaseFormatter) FormatStatus(success bool) string {
	if success {
		return "✓ Success"
	}
	return "✗ Failed"
}

// FormatStatusIcon returns just the status icon
func (f BaseFormatter) FormatStatusIcon(success bool) string {
	if success {
		return "✓"
	}
	return "✗"
}

// FormatDuration formats a duration for display
func (f BaseFormatter) FormatDuration(result *ToolExecutionResult) string {
	return result.Duration.String()
}

// FormatExpandedHeader formats the expanded view header with tool call and metadata
func (f BaseFormatter) FormatExpandedHeader(result *ToolExecutionResult) string {
	var output strings.Builder
	toolCall := f.FormatToolCall(result.Arguments, false)

	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("├─ ⏱️  Duration: %s\n", f.FormatDuration(result)))
	output.WriteString(fmt.Sprintf("├─ 📊 Status: %s\n", f.FormatStatus(result.Success)))

	if result.Error != "" {
		output.WriteString(fmt.Sprintf("├─ ✗ Error: %s\n", result.Error))
	}

	if len(result.Arguments) > 0 {
		output.WriteString("├─ 📝 Arguments:\n")
		keys := make([]string, 0, len(result.Arguments))
		for key := range result.Arguments {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for i, key := range keys {
			value := result.Arguments[key]
			if f.ShouldCollapseArg(key) {
				value = f.collapseArgValue(value, 50)
			}
			hasMore := i < len(keys)-1 || result.Data != nil || len(result.Metadata) > 0
			if hasMore {
				output.WriteString(fmt.Sprintf("│  ├─ %s: %v\n", key, value))
			} else {
				output.WriteString(fmt.Sprintf("│  └─ %s: %v\n", key, value))
			}
		}
	}

	return output.String()
}

// FormatExpandedFooter formats the expanded view footer with metadata
func (f BaseFormatter) FormatExpandedFooter(result *ToolExecutionResult, hasDataSection bool) string {
	if len(result.Metadata) == 0 {
		return ""
	}

	var output strings.Builder
	if hasDataSection {
		output.WriteString("└─ 🏷️  Metadata:\n")
	} else {
		output.WriteString("└─ 🏷️  Metadata:\n")
	}

	keys := make([]string, 0, len(result.Metadata))
	for key := range result.Metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for i, key := range keys {
		if i == len(keys)-1 {
			output.WriteString(fmt.Sprintf("   └─ %s: %s\n", key, result.Metadata[key]))
		} else {
			output.WriteString(fmt.Sprintf("   ├─ %s: %s\n", key, result.Metadata[key]))
		}
	}

	return output.String()
}

// FormatDataSection formats the data section with proper indentation
func (f BaseFormatter) FormatDataSection(dataContent string, hasMetadata bool) string {
	if dataContent == "" {
		return ""
	}

	var output strings.Builder
	if hasMetadata {
		output.WriteString("├─ 📄 Result:\n")
	} else {
		output.WriteString("└─ 📄 Result:\n")
	}

	for _, line := range strings.Split(strings.TrimRight(dataContent, "\n"), "\n") {
		if hasMetadata {
			fmt.Fprintf(&output, "│  %s\n", line)
		} else {
			fmt.Fprintf(&output, "   %s\n", line)
		}
	}

	return output.String()
}

// FormatAsJSON formats data as JSON if possible, falls back to string representation
func (f BaseFormatter) FormatAsJSON(data any) string {
	if jsonData, err := json.MarshalIndent(data, "", "  "); err == nil {
		return string(jsonData)
	}
	return fmt.Sprintf("%+v", data)
}

// ShouldCollapseArg provides default collapse behavior (can be overridden by tools)
func (f BaseFormatter) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldCollapseArg uses the custom collapse function if provided
func (f CustomFormatter) ShouldCollapseArg(key string) bool {
	if f.collapseFunc != nil {
		return f.collapseFunc(key)
	}
	return f.BaseFormatter.ShouldCollapseArg(key)
}

// FormatToolCall overrides BaseFormatter to use custom collapse logic
func (f CustomFormatter) FormatToolCall(args map[string]any, expanded bool) string {
	if len(args) == 0 {
		return fmt.Sprintf("%s()", f.toolName)
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	argPairs := make([]string, 0, len(args))
	for _, key := range keys {
		value := args[key]
		if !expanded && f.ShouldCollapseArg(key) {
			value = "..."
		}
		argPairs = append(argPairs, fmt.Sprintf("%s=%v", key, value))
	}

	return fmt.Sprintf("%s(%s)", f.toolName, f.joinArgs(argPairs))
}

// FormatExpandedHeader overrides BaseFormatter to use custom collapse logic
func (f CustomFormatter) FormatExpandedHeader(result *ToolExecutionResult) string {
	var output strings.Builder
	toolCall := f.FormatToolCall(result.Arguments, false)

	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("├─ ⏱️  Duration: %s\n", f.FormatDuration(result)))
	output.WriteString(fmt.Sprintf("├─ 📊 Status: %s\n", f.FormatStatus(result.Success)))

	if result.Error != "" {
		output.WriteString(fmt.Sprintf("├─ ✗ Error: %s\n", result.Error))
	}

	if len(result.Arguments) > 0 {
		output.WriteString("├─ 📝 Arguments:\n")
		keys := make([]string, 0, len(result.Arguments))
		for key := range result.Arguments {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for i, key := range keys {
			value := result.Arguments[key]
			if f.ShouldCollapseArg(key) {
				value = "..."
			}
			isLast := i == len(keys)-1
			prefix := "│ ├─"
			if isLast {
				prefix = "│ └─"
			}
			output.WriteString(fmt.Sprintf("%s %s: %v\n", prefix, key, value))
		}
	}

	return output.String()
}

// GetFileName extracts filename from a path
func (f BaseFormatter) GetFileName(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

// GetDomainFromURL extracts domain from URL
func (f BaseFormatter) GetDomainFromURL(url string) string {
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")

	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return url
}

// TruncateText truncates text to specified length with ellipsis
func (f BaseFormatter) TruncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}
	if maxLength <= 3 {
		return "..."
	}
	return text[:maxLength-3] + "..."
}

// Private helper methods
func (f BaseFormatter) collapseArgValue(value any, maxLength int) string {
	str := fmt.Sprintf("%v", value)
	if len(str) <= maxLength {
		return str
	}
	if maxLength <= 3 {
		return "..."
	}
	return str[:maxLength-3] + "..."
}

func (f BaseFormatter) joinArgs(args []string) string {
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
