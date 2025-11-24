package shared

import (
	"fmt"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	wordwrap "github.com/muesli/reflow/wordwrap"
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
			wrappedLines := strings.Split(wrapped, "\n")
			for i, wl := range wrappedLines {
				wrappedLines[i] = strings.TrimRight(wl, " ")
			}
			result = append(result, strings.Join(wrappedLines, "\n"))
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
