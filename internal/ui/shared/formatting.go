package shared

import (
	"strings"

	"github.com/muesli/reflow/wordwrap"
)

// WrapText wraps text to fit within the specified width using wordwrap
func WrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	return wordwrap.String(text, width)
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
			result = append(result, wrapped)
		}
	}

	return strings.Join(result, "\n")
}

// GetResponsiveWidth calculates appropriate width based on terminal size
func GetResponsiveWidth(terminalWidth int) int {
	minWidth := 40
	maxWidth := 180

	margin := 4
	availableWidth := terminalWidth - margin

	if availableWidth < minWidth {
		return minWidth
	}

	if availableWidth > maxWidth {
		return maxWidth
	}

	return availableWidth
}

// truncateText truncates text to fit within maxLength, adding "..." if needed
func truncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}

	if maxLength <= 3 {
		return "..."
	}

	return text[:maxLength-3] + "..."
}
