package services

import (
	"fmt"
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

	formatter := domain.NewBaseFormatter(toolName)
	return formatter.FormatToolCall(args, false)
}

// FormatToolCallExpanded formats a tool call with full argument expansion
func (s *ToolFormatterService) FormatToolCallExpanded(toolName string, args map[string]any) string {
	if len(args) == 0 {
		return fmt.Sprintf("%s()", toolName)
	}

	formatter := domain.NewBaseFormatter(toolName)
	return formatter.FormatToolCall(args, true)
}

// FormatToolResultForUI formats tool execution results for UI display
func (s *ToolFormatterService) FormatToolResultForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	tool, err := s.toolRegistry.GetTool(result.ToolName)
	if err != nil {
		return s.formatFallback(result, domain.FormatterUI)
	}

	return tool.FormatResult(result, domain.FormatterUI)
}

// FormatToolResultExpanded formats tool execution results with full details
func (s *ToolFormatterService) FormatToolResultExpanded(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	tool, err := s.toolRegistry.GetTool(result.ToolName)
	if err != nil {
		return s.formatFallback(result, domain.FormatterLLM)
	}

	return tool.FormatResult(result, domain.FormatterLLM)
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

// FormatToolResultForUIResponsive formats with responsive width
func (s *ToolFormatterService) FormatToolResultForUIResponsive(result *domain.ToolExecutionResult, terminalWidth int) string {
	content := s.FormatToolResultForUI(result)
	return s.formatResponsive(content, terminalWidth)
}

// FormatToolResultExpandedResponsive formats expanded results with responsive width
func (s *ToolFormatterService) FormatToolResultExpandedResponsive(result *domain.ToolExecutionResult, terminalWidth int) string {
	content := s.FormatToolResultExpanded(result)
	return s.formatResponsive(content, terminalWidth)
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
	var wrappedLines []string

	for _, line := range lines {
		if len(line) <= terminalWidth {
			wrappedLines = append(wrappedLines, line)
		} else {
			wrapped := s.wrapLongLine(line, terminalWidth)
			wrappedLines = append(wrappedLines, wrapped...)
		}
	}

	return strings.Join(wrappedLines, "\n")
}

// wrapLongLine handles wrapping of a single long line
func (s *ToolFormatterService) wrapLongLine(line string, terminalWidth int) []string {
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{line}
	}

	indent := s.extractIndentation(line)
	var wrappedLines []string
	currentLine := ""

	for _, word := range words {
		if len(currentLine) == 0 {
			currentLine = indent + word
		} else if len(currentLine)+1+len(word) <= terminalWidth {
			currentLine += " " + word
		} else {
			wrappedLines = append(wrappedLines, currentLine)
			currentLine = indent + word
		}
	}

	if currentLine != "" {
		wrappedLines = append(wrappedLines, currentLine)
	}

	return wrappedLines
}

// extractIndentation detects and preserves indentation from the original line
func (s *ToolFormatterService) extractIndentation(line string) string {
	if len(line) == 0 {
		return ""
	}

	if line[0] != ' ' && line[0] != '\t' {
		return ""
	}

	for i, char := range line {
		if char != ' ' && char != '\t' {
			return line[:i]
		}
	}

	return ""
}
