package ui

import "fmt"

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
	return message // UI component will add ❌ automatically
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
