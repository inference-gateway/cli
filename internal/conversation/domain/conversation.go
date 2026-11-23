package domain

import (
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// LastAssistantText returns the trimmed content of the last non-empty assistant
// message in entries, or "" if there is none.
func LastAssistantText(entries []ConversationEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Message.Role != sdk.Assistant {
			continue
		}
		text, err := e.Message.Content.AsMessageContent0()
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(text); s != "" {
			return s
		}
	}
	return ""
}

// ToolOutcomes counts the executed tool calls in entries by outcome. A rejected
// call counts as failed.
func ToolOutcomes(entries []ConversationEntry) (succeeded, failed int) {
	for _, e := range entries {
		switch {
		case e.ToolExecution == nil:
		case e.ToolExecution.Success:
			succeeded++
		default:
			failed++
		}
	}
	return succeeded, failed
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

	const maxTitleLength = 80
	if len(title) > maxTitleLength {
		title = title[:maxTitleLength-len("...")] + "..."
	}

	return title
}

// Origin is how a session started: the orchestrator session that spawned it as
// a subagent, who started it ("human" or "agent"), the Markdown agent it runs
// as, the label its orchestrator named it by, and the session whose message to
// that agent started it.
type Origin struct {
	ParentSessionID string `json:"parent_session_id"`
	InvokedBy       string `json:"invoked_by"`
	Agent           string `json:"agent,omitempty"`
	Label           string `json:"label,omitempty"`
	SentBy          string `json:"sent_by,omitempty"`
}

// ConversationMetadata contains metadata about a conversation. ActiveDuration
// sums the time the agent worked across runs, Origin records how the session
// started, and Project is the absolute working directory backends group
// conversations by.
type ConversationMetadata struct {
	ID                  string            `json:"id"`
	Title               string            `json:"title"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	MessageCount        int               `json:"message_count"`
	TokenStats          SessionTokenStats `json:"token_stats"`
	CostStats           SessionCostStats  `json:"cost_stats,omitempty"`
	Model               string            `json:"model,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
	TitleGenerated      bool              `json:"title_generated,omitempty"`
	TitleInvalidated    bool              `json:"title_invalidated,omitempty"`
	TitleGenerationTime *time.Time        `json:"title_generation_time,omitempty"`
	ContextID           string            `json:"context_id,omitempty"`

	ActiveDuration time.Duration `json:"active_duration,omitempty"`

	Origin

	Project string `json:"project"`
}

// ConversationSummary contains summary information about a conversation, with
// the same Origin and Project as ConversationMetadata.
// Project is empty for legacy records, and Workspace names where a daemon chat
// runs.
type ConversationSummary struct {
	ID                  string            `json:"id"`
	Title               string            `json:"title"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	MessageCount        int               `json:"message_count"`
	TokenStats          SessionTokenStats `json:"token_stats"`
	CostStats           SessionCostStats  `json:"cost_stats,omitempty"`
	Model               string            `json:"model,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
	Summary             string            `json:"summary,omitempty"`
	TitleGenerated      bool              `json:"title_generated,omitempty"`
	TitleInvalidated    bool              `json:"title_invalidated,omitempty"`
	TitleGenerationTime *time.Time        `json:"title_generation_time,omitempty"`

	Origin

	Project string `json:"project,omitempty"`

	Workspace string `json:"workspace,omitempty"`
}
