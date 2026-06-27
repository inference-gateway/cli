package domain

import (
	"strings"
)

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
