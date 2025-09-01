package shared

import (
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
)

// FormatCurrentTokenUsage returns current session token usage string
func FormatCurrentTokenUsage(conversationRepo domain.ConversationRepository) string {
	if conversationRepo == nil {
		return ""
	}

	sessionStats := conversationRepo.GetSessionTokens()
	var parts []string

	parts = append(parts, fmt.Sprintf("Session Input: %d tokens", sessionStats.TotalInputTokens))
	parts = append(parts, fmt.Sprintf("Session Output: %d tokens", sessionStats.TotalOutputTokens))

	return strings.Join(parts, " | ")
}
