package domain

import (
	"strings"
)

// IsFallbackTitle checks if a title appears to be a fallback title that needs AI generation
func IsFallbackTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return true
	}

	title = strings.ToLower(title)

	fallbackTitles := []string{
		"new conversation",
		"conversation",
		"untitled",
	}

	for _, fallback := range fallbackTitles {
		if title == fallback {
			return true
		}
	}

	words := strings.Fields(title)
	if len(words) <= 3 && IsSimpleUserMessage(title) {
		return true
	}

	return false
}

// IsSimpleUserMessage checks if the title looks like a simple user message (hello, hi, help, etc.)
func IsSimpleUserMessage(title string) bool {
	title = strings.ToLower(strings.TrimSpace(title))

	simpleMessages := []string{
		"hi", "hello", "hey", "help", "test", "testing", "ok", "yes", "no", "thanks", "thank you",
		"good morning", "good afternoon", "good evening", "how are you", "what's up",
	}

	for _, simple := range simpleMessages {
		if title == simple {
			return true
		}
	}

	return false
}

// CreateTitleFromMessage creates a short title from message content (fallback title)
func CreateTitleFromMessage(content string) string {
	content = strings.TrimSpace(content)

	words := strings.Fields(content)

	if len(words) == 0 {
		return "New Conversation"
	}

	maxWords := 10
	if len(words) < maxWords {
		maxWords = len(words)
	}

	title := strings.Join(words[:maxWords], " ")

	if len(title) > 80 {
		title = title[:77] + "..."
	}

	return title
}
