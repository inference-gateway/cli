package agui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// digestEntry is what SnapshotDigest hashes per message: the conversation's
// content, never the ids a snapshot mints afresh each time it is written.
type digestEntry struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string     `json:"toolCallId,omitempty"`
}

// SnapshotDigest fingerprints the messages of a MESSAGES_SNAPSHOT by what the
// conversation says, so two daemons can agree that a restored conversation
// matches its checkpoint without comparing message ids.
func SnapshotDigest(messages []Message) string {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	for _, message := range messages {
		_ = encoder.Encode(digestEntry{
			Role:       string(message.Role),
			Content:    message.Content,
			ToolCalls:  message.ToolCalls,
			ToolCallID: message.ToolCallID,
		})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
