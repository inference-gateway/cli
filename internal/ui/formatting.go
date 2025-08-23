package ui

import (
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/internal/ui/shared"
)

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
		return fmt.Sprintf("%s %s", shared.CheckMarkStyle.Render(shared.CheckMark), message)
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

var WrapText = shared.WrapText
var GetResponsiveWidth = shared.GetResponsiveWidth

func FormatResponsiveMessage(message string, terminalWidth int) string {
	width := GetResponsiveWidth(terminalWidth)
	return shared.FormatResponsiveMessage(message, width)
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
