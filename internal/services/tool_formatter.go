package services

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// ToolFormatterService provides formatting for tool results by delegating to individual tools
type ToolFormatterService struct {
	toolRegistry  ToolRegistry
	styleProvider *styles.Provider
	hintFormatter HintProvider
}

// HintProvider resolves keybinding hints for tool result affordances.
// It is satisfied by *hints.Formatter.
type HintProvider interface {
	GetKeyOnly(actionID string) string
}

// SetHintFormatter wires the keybinding hint resolver so the collapsed and expanded
// views can show the "ctrl+o to expand/collapse" affordance. Nil is safe - the hint
// is simply omitted.
func (s *ToolFormatterService) SetHintFormatter(h HintProvider) {
	s.hintFormatter = h
}

func (s *ToolFormatterService) toggleKey() string {
	if s.hintFormatter == nil {
		return ""
	}
	return s.hintFormatter.GetKeyOnly(config.ActionID(config.NamespaceTools, "toggle_tool_expansion"))
}

func (s *ToolFormatterService) expandHint() string {
	if k := s.toggleKey(); k != "" {
		return k + " to expand"
	}
	return ""
}

func (s *ToolFormatterService) collapseHint() string {
	if k := s.toggleKey(); k != "" {
		return k + " to collapse"
	}
	return ""
}

// ToolRegistry interface for accessing tools (implemented by tools.Registry)
type ToolRegistry interface {
	GetTool(name string) (domain.Tool, error)
	ListAvailableTools() []string
}

// NewToolFormatterService creates a new tool formatter service
func NewToolFormatterService(registry ToolRegistry, styleProvider *styles.Provider) *ToolFormatterService {
	return &ToolFormatterService{
		toolRegistry:  registry,
		styleProvider: styleProvider,
	}
}

// FormatToolCall formats a tool call for consistent display
func (s *ToolFormatterService) FormatToolCall(toolName string, args map[string]any) string {
	if len(args) == 0 {
		return fmt.Sprintf("%s()", toolName)
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var shouldCollapseFunc func(string) bool
	tool, err := s.toolRegistry.GetTool(toolName)
	if err == nil {
		shouldCollapseFunc = tool.ShouldCollapseArg
	} else {
		shouldCollapseFunc = func(string) bool { return false }
	}

	argPairs := make([]string, 0, len(args))
	for _, key := range keys {
		value := args[key]
		var formattedValue string
		if shouldCollapseFunc(key) {
			formattedValue = `"..."`
		} else {
			formattedValue = fmt.Sprintf("%v", value)
		}
		argPairs = append(argPairs, fmt.Sprintf("%s=%s", key, formattedValue))
	}

	return fmt.Sprintf("%s(%s)", toolName, s.joinArgs(argPairs))
}

// joinArgs joins argument pairs with commas, handling long argument lists
func (s *ToolFormatterService) joinArgs(args []string) string {
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

// FormatToolResultForUI formats the collapsed (default) tool result: a themed status
// line followed by an indented dim output preview (first 5 lines on success, the full
// output on failure) and a "+N lines · ctrl+o to expand" footer.
func (s *ToolFormatterService) FormatToolResultForUI(result *domain.ToolExecutionResult, terminalWidth int) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	dim := s.styleProvider.GetThemeColor("dim")
	inner := s.cardWidth(terminalWidth) - 4 // minus border + horizontal padding

	if result.Rejected {
		body := s.statusLine(result, terminalWidth)
		if hint := s.expandHint(); hint != "" {
			body += "\n" + s.styleProvider.RenderWithColor(hint, dim)
		}
		return s.wrapCard(result.ToolName, body, terminalWidth)
	}

	lines, more := previewLines(s.resultBody(result), result.Success, inner)

	out := make([]string, 0, len(lines)+3)
	out = append(out, s.statusLine(result, terminalWidth))
	if len(lines) > 0 {
		out = append(out, s.styleProvider.RenderWithColor(strings.Repeat("─", inner), dim))
		for _, ln := range lines {
			out = append(out, s.styleProvider.RenderWithColor(ln, dim))
		}
	}
	if footer := s.collapsedFooter(more); footer != "" {
		out = append(out, s.styleProvider.PlaceHorizontal(inner, "", s.styleProvider.RenderWithColor(footer, dim)))
	}
	return s.wrapCard(result.ToolName, strings.Join(out, "\n"), terminalWidth)
}

// RenderToolSummary renders the shared "<icon> Name(args) <trailing>" line used by the
// collapsed status line, the live preview, the approval summary and the queue preview.
// icon and trailing are already-styled by the caller (each surface owns its own status
// semantics); the tool name and the width-aware, path-shortened argument preview are
// formatted identically everywhere, replacing the per-call-site drift (e.g. the old
// byte-slice `args[:47]` truncation in the live renderer). Pass icon=="" / trailing==""
// to omit either segment.
func (s *ToolFormatterService) RenderToolSummary(icon, toolName string, args map[string]any, trailing string, terminalWidth int) string {
	line := toolName
	if icon != "" {
		line = icon + " " + toolName
	}
	argsPreview := s.formatArgsPreview(args, s.argsPreviewBudget(toolName, terminalWidth))
	if argsPreview != "" && argsPreview != "{}" {
		line += "(" + argsPreview + ")"
	} else {
		line += "()"
	}
	if trailing != "" {
		line += " " + trailing
	}
	return line
}

// statusLine renders the compact "<icon> Name(args) · <duration>" header via the
// shared summary renderer.
func (s *ToolFormatterService) statusLine(result *domain.ToolExecutionResult, terminalWidth int) string {
	icon := icons.CheckMark
	iconColor := "success"
	if !result.Success {
		icon = icons.CrossMark
		iconColor = "error"
	}
	styledIcon := s.styleProvider.RenderWithColor(icon, s.styleProvider.GetThemeColor(iconColor))

	suffix, suffixColor := "· "+formatDurationShort(result.Duration), "dim"
	if result.Rejected {
		suffix, suffixColor = "· Rejected", "error"
	}
	styledSuffix := s.styleProvider.RenderWithColor(suffix, s.styleProvider.GetThemeColor(suffixColor))

	return s.RenderToolSummary(styledIcon, result.ToolName, result.Arguments, styledSuffix, terminalWidth)
}

// cardWidth is the content width for a tool-result card at the given terminal width.
func (s *ToolFormatterService) cardWidth(terminalWidth int) int {
	w := formatting.GetResponsiveWidth(terminalWidth) - 2 // rounded border adds 2 columns
	if w < 20 {
		w = 20
	}
	return w
}

// wrapCard frames content in a rounded card whose top border carries the tool name,
// coloured by tool type (AC8): a subtle border with an accent title, no emoji.
func (s *ToolFormatterService) wrapCard(toolName, content string, terminalWidth int) string {
	return s.styleProvider.RenderTitledCard(
		content,
		toolName,
		s.styleProvider.GetThemeColor("border"),
		s.styleProvider.ToolTitleColor(toolName),
		s.cardWidth(terminalWidth),
	)
}

// argsPreviewBudget is the width available for the inline argument preview on the
// collapsed status line, after reserving room for the icon, tool name, parentheses
// and the trailing duration. It scales with the terminal width so long values (e.g.
// bash commands) stay readable on wide terminals instead of being clipped to a fixed
// cap; the full value is always available via ctrl+o.
func (s *ToolFormatterService) argsPreviewBudget(toolName string, terminalWidth int) int {
	const (
		reserved = 18
		minimum  = 50
	)
	budget := formatting.GetResponsiveWidth(terminalWidth) - len(toolName) - reserved
	if budget < minimum {
		return minimum
	}
	return budget
}

// formatArgsPreview formats arguments for a compact one-line preview, truncating
// each value and the joined result to maxWidth (the collapsed status line's budget).
func (s *ToolFormatterService) formatArgsPreview(args map[string]any, maxWidth int) string {
	if len(args) == 0 {
		return ""
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var argPairs []string
	for _, key := range keys {
		valueStr := strings.ReplaceAll(fmt.Sprintf("%v", args[key]), "\n", " ")
		valueStr = s.shortenPathInValue(valueStr)
		valueStr = formatting.TruncateText(valueStr, maxWidth)
		argPairs = append(argPairs, fmt.Sprintf("%s=%s", key, valueStr))
	}

	return formatting.TruncateText(strings.Join(argPairs, ", "), maxWidth)
}

// FormatToolResultExpanded formats the expanded (ctrl+o) tool result: the tool's
// existing detail tree, themed for consistency with the rest of the UI (accent+bold
// tool-call line, dim connectors, accent field labels), with a dim collapse hint.
// The underlying tree text is unchanged, so tool-specific bodies (diffs, raw output)
// are preserved exactly.
func (s *ToolFormatterService) FormatToolResultExpanded(result *domain.ToolExecutionResult, terminalWidth int) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var tree string
	if tool, err := s.toolRegistry.GetTool(result.ToolName); err != nil {
		tree = s.formatFallback(result, domain.FormatterLLM)
	} else {
		tree = safeToolFormat(result.ToolName, func() string { return tool.FormatResult(result, domain.FormatterLLM) })
	}

	inner := s.cardWidth(terminalWidth) - 4 // minus border + horizontal padding
	tree = wrapTreeLines(tree, inner)
	body := s.themeTreeLines(tree)
	if hint := s.collapseHintLine(result); hint != "" {
		body += "\n" + hint
	}
	return s.wrapCard(result.ToolName, body, terminalWidth)
}

// FormatToolResultForLLM formats tool execution results for LLM consumption
func (s *ToolFormatterService) FormatToolResultForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	tool, err := s.toolRegistry.GetTool(result.ToolName)
	if err != nil {
		return s.formatFallback(result, domain.FormatterLLM)
	}

	return safeToolFormat(result.ToolName, func() string { return tool.FormatResult(result, domain.FormatterLLM) })
}

// formatFallback provides fallback formatting when tool is not available
func (s *ToolFormatterService) formatFallback(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	formatter := domain.NewBaseFormatter(result.ToolName)

	switch formatType {
	case domain.FormatterUI:
		if s.isGatewayToolWithEnhancedVisualization(result) {
			return s.formatEnhancedGatewayTool(result, &formatter)
		}

		toolCall := formatter.FormatToolCall(result.Arguments, false)
		statusIcon := formatter.FormatStatusIcon(result.Success)
		statusText := "completed"
		if !result.Success {
			statusText = "failed"
		}

		return fmt.Sprintf("%s %s %s", statusIcon, toolCall, statusText)

	case domain.FormatterLLM:
		var dataContent string
		if result.Data != nil {
			dataContent = formatter.FormatAsJSON(result.Data)
		}
		return formatter.FormatExpanded(result, dataContent)

	default:
		if result.Success {
			return "Execution completed successfully"
		}
		return "Execution failed"
	}
}

// shortenPathInValue converts absolute file paths to relative paths for compact display.
// If the value looks like an absolute path (starts with /), it's converted to a relative
// path from the working directory. Falls back to just the filename if the path is not
// under the working directory. Non-path values are returned unchanged.
func (s *ToolFormatterService) shortenPathInValue(value string) string {
	if !strings.HasPrefix(value, "/") {
		return value
	}
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Base(value)
	}
	rel, err := filepath.Rel(wd, value)
	if err != nil {
		return filepath.Base(value)
	}
	if len(rel) < len(value) {
		return rel
	}
	return filepath.Base(value)
}

// ShouldAlwaysExpandTool checks if a tool result should always be expanded
func (s *ToolFormatterService) ShouldAlwaysExpandTool(toolName string) bool {
	tool, err := s.toolRegistry.GetTool(toolName)
	if err != nil {
		return false
	}
	return tool.ShouldAlwaysExpand()
}

// isGatewayToolWithEnhancedVisualization checks if this is a Gateway tool with enhanced visualization
func (s *ToolFormatterService) isGatewayToolWithEnhancedVisualization(result *domain.ToolExecutionResult) bool {
	if result.Data == nil {
		return false
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		return false
	}

	friendlyFormat, exists := data["friendly_format"]
	if !exists {
		return false
	}

	return friendlyFormat == true
}

// formatEnhancedGatewayTool formats Gateway tools with enhanced user-friendly visualization
func (s *ToolFormatterService) formatEnhancedGatewayTool(result *domain.ToolExecutionResult, formatter *domain.BaseFormatter) string {
	data, ok := result.Data.(map[string]any)
	if !ok {
		return fmt.Sprintf("%s Executed on Gateway", formatter.FormatStatusIcon(result.Success))
	}
	visualDisplay, _ := data["visual_display"].(string)
	statusIcon := formatter.FormatStatusIcon(result.Success)

	toolType, _ := data["type"].(string)

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", visualDisplay)

	switch toolType {
	case "A2A":
		fmt.Fprintf(&output, "└─ %s 🔗 Delegated to A2A Agent on Gateway", statusIcon)
	case "MCP":
		fmt.Fprintf(&output, "└─ %s 🔧 Executed via MCP on Gateway", statusIcon)
	default:
		fmt.Fprintf(&output, "└─ %s Executed on Gateway", statusIcon)
	}

	return output.String()
}
