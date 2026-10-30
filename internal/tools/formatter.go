package tools

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	tree "charm.land/lipgloss/v2/tree"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Status glyphs shared by the formatters. Plain runes, no styling: color is
// applied by the UI layer (toolformatter.ToolFormatterService), never here.
const (
	checkMark = "✓"
	crossMark = "✗"
)

// BaseFormatter provides common formatting functionality that tools can embed.
// Its collapse rule, when set, hides the matching arguments behind "...".
type BaseFormatter struct {
	toolName     string
	collapseFunc func(string) bool
}

// NewBaseFormatter creates a formatter that shows every argument
func NewBaseFormatter(toolName string) BaseFormatter {
	return BaseFormatter{toolName: toolName}
}

// NewCustomFormatter creates a formatter with custom collapse logic
func NewCustomFormatter(toolName string, collapseFunc func(string) bool) BaseFormatter {
	return BaseFormatter{toolName: toolName, collapseFunc: collapseFunc}
}

// FormatToolCall formats a tool call for consistent display
func (f BaseFormatter) FormatToolCall(args map[string]any, expanded bool) string {
	argPairs := make([]string, 0, len(args))
	for _, key := range slices.Sorted(maps.Keys(args)) {
		value := args[key]
		if !expanded && f.ShouldCollapseArg(key) {
			value = "..."
		}
		argPairs = append(argPairs, fmt.Sprintf("%s=%v", key, value))
	}
	return fmt.Sprintf("%s(%s)", f.toolName, strings.Join(argPairs, ", "))
}

// FormatStatusIcon returns just the status icon
func (f BaseFormatter) FormatStatusIcon(success bool) string {
	if success {
		return checkMark
	}
	return crossMark
}

// FormatDuration formats a duration for display in a human-friendly way
func (f BaseFormatter) FormatDuration(result *agentdomain.ToolExecutionResult) string {
	d := result.Duration

	if d < 1e3 {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < 1e6 {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < 1e9 {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < 60e9 {
		return fmt.Sprintf("%ds", int64(d.Seconds()))
	}

	if d < 3600e9 {
		minutes := int64(d.Minutes())
		seconds := int64(d.Seconds()) % 60
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}

	hours := int64(d.Hours())
	minutes := int64(d.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

// FormatAsJSON formats data as JSON if possible, falls back to string representation
func (f BaseFormatter) FormatAsJSON(data any) string {
	if jsonData, err := json.MarshalIndent(data, "", "  "); err == nil {
		return string(jsonData)
	}
	return fmt.Sprintf("%+v", data)
}

// ShouldCollapseArg reports whether the collapse rule hides the argument
func (f BaseFormatter) ShouldCollapseArg(key string) bool {
	return f.collapseFunc != nil && f.collapseFunc(key)
}

// FormatExpanded renders the full expanded result as a single native lipgloss/tree.
// dataContent is the tool-specific result body (may be ""). Output is plain: the UI
// wraps and themes it (services.themeTreeLines), LLM and headless consume it as-is.
func (f BaseFormatter) FormatExpanded(result *agentdomain.ToolExecutionResult, dataContent string) string {
	return renderExpandedTree(f.FormatToolCall(result.Arguments, false), result, dataContent, f.argValue)
}

// FormatCollapsed renders the collapsed view of a result under the tool call.
func (f BaseFormatter) FormatCollapsed(result *agentdomain.ToolExecutionResult, summary string) string {
	return renderCollapsed(f.FormatToolCall(result.Arguments, false), result.Success, summary)
}

// argValue formats a single argument value for the expanded view, honoring the
// formatter's collapse rule.
func (f BaseFormatter) argValue(key string, value any) string {
	if f.ShouldCollapseArg(key) {
		return "..."
	}
	return fmt.Sprintf("%v", value)
}

// renderExpandedTree builds the shared native tree from a result. It is the single
// source of the ├──/╰── connectors, replacing the per-method hand drawing.
func renderExpandedTree(toolCall string, result *agentdomain.ToolExecutionResult, dataContent string, argValue func(string, any) string) string {
	base := BaseFormatter{}
	t := tree.Root(toolCall).Enumerator(tree.RoundedEnumerator)

	status := checkMark + " Success"
	if !result.Success {
		status = crossMark + " Failed"
	}
	t.Child("Duration: " + base.FormatDuration(result))
	t.Child("Status: " + status)
	if result.Error != "" {
		t.Child("Error: " + result.Error)
	}

	if len(result.Arguments) > 0 {
		args := tree.Root("Arguments:")
		for _, key := range slices.Sorted(maps.Keys(result.Arguments)) {
			args.Child(fmt.Sprintf("%s: %s", key, argValue(key, result.Arguments[key])))
		}
		t.Child(args)
	}

	if dataContent != "" {
		t.Child("Result:\n" + strings.TrimRight(dataContent, "\n"))
	}

	if len(result.Metadata) > 0 {
		meta := tree.Root("Metadata:")
		for _, key := range slices.Sorted(maps.Keys(result.Metadata)) {
			meta.Child(fmt.Sprintf("%s: %v", key, result.Metadata[key]))
		}
		t.Child(meta)
	}

	return trimLinePadding(t.String())
}

// renderCollapsed draws the collapsed view: the tool call, then the status and
// summary under one connector. Later summary lines are indented under the first
// so a multi-line summary stays inside the block.
func renderCollapsed(toolCall string, success bool, summary string) string {
	status := crossMark
	if success {
		status = checkMark
	}
	lines := strings.Split(summary, "\n")
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n└─ %s %s", toolCall, status, lines[0])
	for _, line := range lines[1:] {
		fmt.Fprintf(&out, "\n     %s", line)
	}
	return out.String()
}

// trimLinePadding drops the trailing spaces lipgloss adds to pad every line of
// a multi-line node to its widest line, so a tool result keeps its own line
// lengths instead of sending a block of whitespace to the model.
func trimLinePadding(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// GetFileName extracts filename from a path
func (f BaseFormatter) GetFileName(path string) string {
	return filepath.Base(path)
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
