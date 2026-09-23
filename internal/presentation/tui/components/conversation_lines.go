package components

import (
	"fmt"
	"strings"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

// ConversationLineFormatter converts conversation entries to text lines
type ConversationLineFormatter struct {
	width         int
	toolFormatter tui.ToolFormatter
}

// NewConversationLineFormatter creates a new conversation line formatter
func NewConversationLineFormatter(width int, toolFormatter tui.ToolFormatter) *ConversationLineFormatter {
	return &ConversationLineFormatter{
		width:         width,
		toolFormatter: toolFormatter,
	}
}

// SetWidth updates the formatter width
func (f *ConversationLineFormatter) SetWidth(width int) {
	f.width = width
}

// FormatConversationToLines converts conversation entries to plain text lines
func (f *ConversationLineFormatter) FormatConversationToLines(conversation []convdomain.ConversationEntry) []string {
	var lines []string

	for _, entry := range conversation {
		if entry.Hidden {
			continue
		}

		var role, content string
		switch string(entry.Message.Role) {
		case "user":
			role = "> You"
		case "assistant":
			if entry.Model != "" {
				role = fmt.Sprintf("⏺ %s", entry.Model)
			} else {
				role = "⏺ Assistant"
			}
		case "tool":
			role = "Tool"
		default:
			role = string(entry.Message.Role)
		}

		contentStr, err := entry.Message.Content.AsMessageContent0()
		if err != nil {
			contentStr = formatting.ExtractTextFromContent(entry.Message.Content, entry.Images)
		}
		content = contentStr
		message := fmt.Sprintf("%s: %s", role, content)

		entryLines := strings.Split(message, "\n")
		for _, line := range entryLines {
			lines = append(lines, strings.TrimRight(line, " "))
		}
		lines = append(lines, "")
	}

	return lines
}
