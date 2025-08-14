package ui

import (
	"fmt"
	"sort"
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
		return fmt.Sprintf("✅ %s", message)
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
	return fmt.Sprintf("✅ %s", message)
}

// FormatWarning creates a properly formatted warning message
func FormatWarning(message string) string {
	return fmt.Sprintf("⚠️ %s", message)
}

// FormatErrorCLI creates an error message with ❌ prefix for CLI output
func FormatErrorCLI(message string) string {
	return fmt.Sprintf("❌ %s", message)
}

// FormatToolCall formats a tool call for consistent display across the application
func FormatToolCall(toolName string, args map[string]interface{}) string {
	if len(args) == 0 {
		return fmt.Sprintf("%s()", toolName)
	}

	var argPairs []string
	for key, value := range args {
		argPairs = append(argPairs, fmt.Sprintf("%s=%v", key, value))
	}

	sort.Strings(argPairs)

	return fmt.Sprintf("%s(%s)", toolName, joinArgs(argPairs))
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
