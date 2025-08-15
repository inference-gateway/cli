package shared

import (
	"strings"
	"unicode"

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

// stripANSI removes ANSI escape sequences from text
func stripANSI(text string) string {
	var result strings.Builder
	inEscape := false
	
	for _, r := range text {
		if r == '\033' {
			inEscape = true
			continue
		}
		
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		
		result.WriteRune(r)
	}
	
	return result.String()
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

// isWhitespace checks if a rune is whitespace
func isWhitespace(r rune) bool {
	return unicode.IsSpace(r)
}