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

	if sessionStats.TotalInputTokens > 0 {
		parts = append(parts, fmt.Sprintf("Session Input: %d tokens", sessionStats.TotalInputTokens))
	}

	if sessionStats.TotalOutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("Session Output: %d tokens", sessionStats.TotalOutputTokens))
	}

	if len(parts) > 0 {
		return strings.Join(parts, " | ")
	}

	return "Session Input: 0 tokens | Session Output: 0 tokens"
}
