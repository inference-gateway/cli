package formatting

import (
	"fmt"
	"sort"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
	wordwrap "github.com/muesli/reflow/wordwrap"
)

// ============================================================================
// Text Utilities
// ============================================================================

// WrapText wraps text to fit within the specified width using wordwrap
func WrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	return wordwrap.String(text, width)
}

// GetResponsiveWidth calculates appropriate width based on terminal size
func GetResponsiveWidth(terminalWidth int) int {
	const (
		minWidth    = 40
		maxWidth    = 150
		leftPadding = 2
		rightBuffer = 6
		margin      = leftPadding + rightBuffer
	)

	availableWidth := terminalWidth - margin

	if availableWidth < minWidth {
		return minWidth
	}

	if availableWidth > maxWidth {
		return maxWidth
	}

	return availableWidth
}

// FormatResponsiveMessage formats a message with responsive text wrapping
func FormatResponsiveMessage(content string, width int) string {
	if width <= 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	var result []string

	for _, line := range lines {
		if len(line) <= width {
			result = append(result, line)
		} else {
			wrapped := WrapText(line, width)
			wrappedLines := strings.Split(wrapped, "\n")
			for i, wl := range wrappedLines {
				wrappedLines[i] = strings.TrimRight(wl, " ")
			}
			result = append(result, strings.Join(wrappedLines, "\n"))
		}
	}

	return strings.Join(result, "\n")
}

// FormatResponsiveCodeBlock formats code blocks with responsive width
func FormatResponsiveCodeBlock(code string, terminalWidth int) string {
	width := GetResponsiveWidth(terminalWidth)
	lines := strings.Split(code, "\n")
	var wrappedLines []string

	for _, line := range lines {
		if len(line) > width {
			wrappedLines = append(wrappedLines, WrapText(line, width))
		} else {
			wrappedLines = append(wrappedLines, line)
		}
	}

	return strings.Join(wrappedLines, "\n")
}

// TruncateText truncates text to fit within maxLength, adding "..." if needed
func TruncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}

	if maxLength <= 3 {
		return "..."
	}

	return text[:maxLength-3] + "..."
}

// ExtractTextFromContent extracts text from potentially multimodal message content
func ExtractTextFromContent(content sdk.MessageContent, images []domain.ImageAttachment) string {
	simpleStr, err := content.AsMessageContent0()
	if err == nil {
		return simpleStr
	}

	multimodalContent, err := content.AsMessageContent1()
	if err != nil {
		if len(images) > 0 {
			var parts []string
			for i := range images {
				parts = append(parts, fmt.Sprintf("[Image %d]", i+1))
			}
			return strings.Join(parts, " ")
		}
		return "[error extracting content]"
	}

	var textParts []string
	imageCount := 0
	for _, part := range multimodalContent {
		if textPart, err := part.AsTextContentPart(); err == nil {
			textParts = append(textParts, textPart.Text)
			continue
		}

		if _, err := part.AsImageContentPart(); err == nil {
			imageCount++
			textParts = append(textParts, fmt.Sprintf("[Image %d]", imageCount))
		}
	}

	if len(textParts) > 0 {
		return strings.Join(textParts, " ")
	}

	if len(images) > 0 {
		var parts []string
		for i := range images {
			parts = append(parts, fmt.Sprintf("[Image %d]", i+1))
		}
		return strings.Join(parts, " ")
	}

	return "[empty message]"
}

// ============================================================================
// Message Formatting
// ============================================================================

// MessageType represents different types of messages
type MessageType int

const (
	MessageSuccess MessageType = iota
	MessageError
	MessageWarning
	MessageInfo
	MessageProgress
)

// FormatMessage formats a message with appropriate icons and styling
func FormatMessage(msgType MessageType, message string) string {
	switch msgType {
	case MessageSuccess:
		return fmt.Sprintf("%s %s", icons.CheckMarkStyle.Render(icons.CheckMark), message)
	case MessageError:
		return message
	case MessageWarning:
		return fmt.Sprintf("⚠️ %s", message)
	case MessageInfo:
		return message
	case MessageProgress:
		return message
	default:
		return message
	}
}

// FormatError creates a properly formatted error message without duplicate symbols
func FormatError(message string) string {
	return message
}

// FormatSuccess creates a properly formatted success message
func FormatSuccess(message string) string {
	return fmt.Sprintf("\033[32m%s\033[0m", message)
}

// FormatWarning creates a properly formatted warning message
func FormatWarning(message string) string {
	return fmt.Sprintf("\033[33m%s\033[0m", message)
}

// FormatErrorCLI creates an error message with red color for CLI output
func FormatErrorCLI(message string) string {
	return fmt.Sprintf("\033[31m%s\033[0m", message)
}

// FormatEnabled formats an enabled status
func FormatEnabled() string {
	return FormatSuccess("ENABLED")
}

// FormatDisabled formats a disabled status
func FormatDisabled() string {
	return FormatErrorCLI("DISABLED")
}

// ============================================================================
// Tool Formatting
// ============================================================================

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

// ============================================================================
// Cost Formatting
// ============================================================================

// FormatCost formats cost with adaptive precision based on magnitude
// Returns "-" for zero cost, and uses 2-4 decimal places based on the amount
func FormatCost(cost float64) string {
	if cost == 0 {
		return "-"
	} else if cost < 0.01 {
		return fmt.Sprintf("$%.4f", cost)
	} else if cost < 1.0 {
		return fmt.Sprintf("$%.3f", cost)
	} else {
		return fmt.Sprintf("$%.2f", cost)
	}
}
