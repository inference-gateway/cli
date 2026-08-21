package conversation

import (
	"strings"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// BuildAgentMessagesFromEntries converts conversation entries into the flat
// slice of SDK messages sent to the model, dropping the three UI-only classes:
// plan entries, user-typed `!command` entries, and pending-approval placeholders.
// Plan and bash entries carry no reasoning_content, which thinking-mode providers
// reject with HTTP 400; a placeholder between an assistant tool_calls message and
// its tool response breaks provider adjacency.
func BuildAgentMessagesFromEntries(entries []convdomain.ConversationEntry) []sdk.Message {
	messages := make([]sdk.Message, 0, len(entries))
	for _, entry := range entries {
		if entry.IsPlan {
			continue
		}
		if isUserInitiatedBashEntry(entry) {
			continue
		}
		if entry.PendingToolCall != nil {
			continue
		}
		msg := entry.Message
		if entry.ReasoningContent != "" && msg.Reasoning == nil && msg.ReasoningContent == nil {
			rc := entry.ReasoningContent
			msg.Reasoning = &rc
			msg.ReasoningContent = &rc
		}
		messages = append(messages, msg)
	}
	return messages
}

// isUserInitiatedBashEntry reports whether the entry was synthesized for a
// user-typed `!command` shortcut. Tool-call IDs created by that path are
// prefixed with `user-bash-` (see DirectExecutionService).
func isUserInitiatedBashEntry(entry convdomain.ConversationEntry) bool {
	const userBashPrefix = "user-bash-"

	if entry.Message.ToolCallID != nil && strings.HasPrefix(*entry.Message.ToolCallID, userBashPrefix) {
		return true
	}

	if entry.Message.ToolCalls != nil {
		for _, tc := range *entry.Message.ToolCalls {
			if strings.HasPrefix(tc.ID, userBashPrefix) {
				return true
			}
		}
	}
	return false
}
