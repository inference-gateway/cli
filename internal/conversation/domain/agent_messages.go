package domain

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

const (
	userBashPrefix = "user-bash-"
	userToolPrefix = "user-tool-"
)

// NewUserBashCallID names the tool call a user-typed `!command` records.
func NewUserBashCallID() string {
	return userBashPrefix + strconv.FormatInt(time.Now().UnixNano(), 10)
}

// NewUserToolCallID names the tool call a user-typed `!!Tool(...)` records.
func NewUserToolCallID() string {
	return userToolPrefix + strconv.FormatInt(time.Now().UnixNano(), 10)
}

// IsUserBashCall reports whether id names the tool call of a user-typed `!command`.
func IsUserBashCall(id string) bool {
	return strings.HasPrefix(id, userBashPrefix)
}

// NewUserBashOutputEntry carries a user-typed `!command`'s output to the model:
// its tool pair is dropped from LLM history, while a hidden entry stays out of
// the view and still reaches the model.
func NewUserBashOutputEntry(command, output string, now time.Time) ConversationEntry {
	return ConversationEntry{
		Message: sdk.Message{
			Role: sdk.User,
			Content: sdk.NewMessageContent(fmt.Sprintf(
				"<system-reminder>\nOutput of the shell command the user ran directly with `!%s`:\n%s\n</system-reminder>", command, output)),
		},
		Time:   now,
		Hidden: true,
	}
}

// BuildAgentMessagesFromEntries converts conversation entries into the flat
// slice of SDK messages sent to the model, dropping the three UI-only classes:
// plan entries, user-typed `!command` entries, and pending-approval placeholders.
// Plan and bash entries carry no reasoning_content, which thinking-mode providers
// reject with HTTP 400; a placeholder between an assistant tool_calls message and
// its tool response breaks provider adjacency.
func BuildAgentMessagesFromEntries(entries []ConversationEntry) []sdk.Message {
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
// user-typed `!command` shortcut, by its NewUserBashCallID tool-call ID.
func isUserInitiatedBashEntry(entry ConversationEntry) bool {
	if entry.Message.ToolCallID != nil && IsUserBashCall(*entry.Message.ToolCallID) {
		return true
	}

	if entry.Message.ToolCalls != nil {
		for _, tc := range *entry.Message.ToolCalls {
			if IsUserBashCall(tc.ID) {
				return true
			}
		}
	}
	return false
}
