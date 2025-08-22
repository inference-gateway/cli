package services

import (
	"fmt"
	"sort"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
)

// ToolFormatterService provides formatting for tool results by delegating to individual tools
type ToolFormatterService struct {
	toolRegistry ToolRegistry
}

// ToolRegistry interface for accessing tools (implemented by tools.Registry)
type ToolRegistry interface {
	GetTool(name string) (domain.Tool, error)
	ListAvailableTools() []string
}

// NewToolFormatterService creates a new tool formatter service
func NewToolFormatterService(registry ToolRegistry) *ToolFormatterService {
	return &ToolFormatterService{
		toolRegistry: registry,
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
	sort.Strings(keys)

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

// FormatToolResultForUI formats tool execution results for UI display
func (s *ToolFormatterService) FormatToolResultForUI(result *domain.ToolExecutionResult, terminalWidth int) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	tool, err := s.toolRegistry.GetTool(result.ToolName)
	if err != nil {
		content := s.formatFallback(result, domain.FormatterUI)
		return s.formatResponsive(content, terminalWidth)
	}

	content := tool.FormatResult(result, domain.FormatterUI)
	return s.formatResponsive(content, terminalWidth)
}

// FormatToolResultExpanded formats expanded tool execution results
func (s *ToolFormatterService) FormatToolResultExpanded(result *domain.ToolExecutionResult, terminalWidth int) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	tool, err := s.toolRegistry.GetTool(result.ToolName)
	if err != nil {
		content := s.formatFallback(result, domain.FormatterLLM)
		return s.formatResponsive(content, terminalWidth)
	}

	content := tool.FormatResult(result, domain.FormatterLLM)
	return s.formatResponsive(content, terminalWidth)
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

	return tool.FormatResult(result, domain.FormatterLLM)
}

// formatFallback provides fallback formatting when tool is not available
func (s *ToolFormatterService) formatFallback(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	formatter := domain.NewBaseFormatter(result.ToolName)

	switch formatType {
	case domain.FormatterUI:
		toolCall := formatter.FormatToolCall(result.Arguments, false)
		statusIcon := formatter.FormatStatusIcon(result.Success)
		preview := "Execution completed"
		if !result.Success {
			preview = "Execution failed"
		}

		var output strings.Builder
		output.WriteString(fmt.Sprintf("%s\n", toolCall))
		output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, preview))
		return output.String()

	case domain.FormatterLLM:
		var output strings.Builder
		output.WriteString(formatter.FormatExpandedHeader(result))
		if result.Data != nil {
			dataContent := formatter.FormatAsJSON(result.Data)
			hasMetadata := len(result.Metadata) > 0
			output.WriteString(formatter.FormatDataSection(dataContent, hasMetadata))
		}
		hasDataSection := result.Data != nil
		output.WriteString(formatter.FormatExpandedFooter(result, hasDataSection))
		return output.String()

	default:
		if result.Success {
			return "Execution completed successfully"
		}
		return "Execution failed"
	}
}

// formatResponsive handles text wrapping for responsive display
func (s *ToolFormatterService) formatResponsive(content string, terminalWidth int) string {
	if terminalWidth <= 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	var result []string

	for _, line := range lines {
		if len(line) <= terminalWidth {
			result = append(result, line)
		} else {
			wrapped := s.wrapText(line, terminalWidth)
			result = append(result, wrapped)
		}
	}

	return strings.Join(result, "\n")
}

// isFormattedLineNumberText checks if text appears to be formatted with line numbers
func (s *ToolFormatterService) isFormattedLineNumberText(text string) bool {
	if !strings.Contains(text, "\t") || len(text) == 0 {
		return false
	}

	if text[0] != ' ' && (text[0] < '0' || text[0] > '9') {
		return false
	}

	trimmed := strings.TrimLeft(text, " ")
	if len(trimmed) == 0 {
		return false
	}

	tabIndex := strings.Index(trimmed, "\t")
	if tabIndex <= 0 {
		return false
	}

	prefix := trimmed[:tabIndex]
	for _, r := range prefix {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// wrapText wraps text using word wrapping, but preserves formatted content like line numbers
func (s *ToolFormatterService) wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	if s.isFormattedLineNumberText(text) {
		return text
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}

	var lines []string
	currentLine := ""

	for _, word := range words {
		if len(currentLine) == 0 {
			currentLine = word
		} else if len(currentLine)+1+len(word) <= width {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return strings.Join(lines, "\n")
}

// FormatToolArgumentsForApproval formats tool arguments for approval display
// This delegates to individual tools if they have special formatting needs
func (s *ToolFormatterService) FormatToolArgumentsForApproval(toolName string, args map[string]any) string {
	tool, err := s.toolRegistry.GetTool(toolName)
	if err != nil {
		return s.formatGenericArguments(args)
	}

	if approvalFormatter, ok := tool.(ApprovalArgumentFormatter); ok {
		return approvalFormatter.FormatArgumentsForApproval(args)
	}

	return s.formatGenericArguments(args)
}

// ApprovalArgumentFormatter interface for tools that need custom approval argument formatting
type ApprovalArgumentFormatter interface {
	FormatArgumentsForApproval(args map[string]any) string
}

// formatGenericArguments provides the default argument formatting for approval view
func (s *ToolFormatterService) formatGenericArguments(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}

	var result strings.Builder
	result.WriteString("Arguments:\n")

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for i, key := range keys {
		value := args[key]
		result.WriteString(fmt.Sprintf("  • %s: %v", key, value))
		if i < len(keys)-1 {
			result.WriteString("\n")
		}
	}

	return result.String()
}

// ShouldAlwaysExpandTool checks if a tool result should always be expanded
func (s *ToolFormatterService) ShouldAlwaysExpandTool(toolName string) bool {
	tool, err := s.toolRegistry.GetTool(toolName)
	if err != nil {
		return false
	}
	return tool.ShouldAlwaysExpand()
}
