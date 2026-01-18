package services

import (
	"fmt"
	"sort"
	"strings"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// ToolFormatterService provides formatting for tool results by delegating to individual tools
type ToolFormatterService struct {
	toolRegistry  ToolRegistry
	styleProvider *styles.Provider
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
// Uses single-line format with colors (consolidated with live preview format)
func (s *ToolFormatterService) FormatToolResultForUI(result *domain.ToolExecutionResult, terminalWidth int) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	logger.Debug("FormatToolResultForUI called",
		"tool", result.ToolName,
		"success", result.Success,
		"duration_ms", result.Duration.Milliseconds(),
		"arguments", result.Arguments)

	var statusIcon string
	var statusText string
	var iconColor string
	var statusColor string

	if result.Success {
		statusIcon = icons.CheckMark
		duration := result.Duration
		statusText = fmt.Sprintf("completed in %s", s.formatDuration(duration))
		iconColor = "success"
		statusColor = "dim"
	} else {
		statusIcon = icons.CrossMark
		duration := result.Duration
		statusText = fmt.Sprintf("failed after %s", s.formatDuration(duration))
		iconColor = "error"
		statusColor = "error"
	}

	argsPreview := s.formatArgsPreview(result.Arguments)

	styledIcon := s.styleProvider.RenderWithColor(statusIcon, s.styleProvider.GetThemeColor(iconColor))
	styledStatus := s.styleProvider.RenderWithColor(statusText, s.styleProvider.GetThemeColor(statusColor))

	var singleLine string
	if argsPreview != "" && argsPreview != "{}" {
		singleLine = fmt.Sprintf("%s %s(%s) %s", styledIcon, result.ToolName, argsPreview, styledStatus)
	} else {
		singleLine = fmt.Sprintf("%s %s() %s", styledIcon, result.ToolName, styledStatus)
	}

	logger.Debug("Tool formatted result (single-line)", "tool", result.ToolName, "result_length", len(singleLine))
	return singleLine
}

// formatArgsPreview formats arguments for compact preview display
func (s *ToolFormatterService) formatArgsPreview(args map[string]any) string {
	logger.Debug("formatArgsPreview called", "args", args, "len", len(args))

	if len(args) == 0 {
		logger.Debug("formatArgsPreview: args empty, returning empty string")
		return ""
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var argPairs []string
	for _, key := range keys {
		value := args[key]
		// Truncate long values
		valueStr := fmt.Sprintf("%v", value)
		if len(valueStr) > 50 {
			valueStr = valueStr[:47] + "..."
		}
		argPairs = append(argPairs, fmt.Sprintf("%s=%s", key, valueStr))
	}

	preview := strings.Join(argPairs, ", ")
	if len(preview) > 100 {
		preview = preview[:97] + "..."
	}

	logger.Debug("formatArgsPreview result", "preview", preview, "length", len(preview))
	return preview
}

// formatDuration formats a duration in human-readable way
func (s *ToolFormatterService) formatDuration(d time.Duration) string {
	seconds := d.Seconds()
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	minutes := int(seconds / 60)
	remainingSeconds := seconds - float64(minutes*60)
	return fmt.Sprintf("%dm%.1fs", minutes, remainingSeconds)
}

// FormatToolResultExpanded formats expanded tool execution results
func (s *ToolFormatterService) FormatToolResultExpanded(result *domain.ToolExecutionResult, terminalWidth int) string {
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
	data := result.Data.(map[string]any)
	visualDisplay := data["visual_display"].(string)
	statusIcon := formatter.FormatStatusIcon(result.Success)

	toolType, _ := data["type"].(string)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", visualDisplay))

	switch toolType {
	case "A2A":
		output.WriteString(fmt.Sprintf("└─ %s 🔗 Delegated to A2A Agent on Gateway", statusIcon))
	case "MCP":
		output.WriteString(fmt.Sprintf("└─ %s 🔧 Executed via MCP on Gateway", statusIcon))
	default:
		output.WriteString(fmt.Sprintf("└─ %s Executed on Gateway", statusIcon))
	}

	return output.String()
}
