package domain

import (
	"strings"
	"time"
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

// ConversationMetadata contains metadata about a conversation
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

	// ParentSessionID is the orchestrator session that spawned this one
	// (e.g. a subagent's parent). Empty for top-level sessions.
	ParentSessionID string `json:"parent_session_id"`

	// InvokedBy indicates who started the session: "human" or "agent".
	// Defaults to "human" for sessions with no explicit invoker.
	InvokedBy string `json:"invoked_by"`

	// Project is the absolute path of the working directory the session
	// runs in. Storage backends group conversations by it so listing can
	// be scoped to the current project.
	Project string `json:"project"`
}

// ConversationSummary contains summary information about a conversation
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

	// ParentSessionID is the orchestrator session that spawned this one.
	ParentSessionID string `json:"parent_session_id"`

	// InvokedBy indicates who started the session: "human" or "agent".
	InvokedBy string `json:"invoked_by"`

	// Project is the absolute path of the working directory the session runs
	// in; empty for legacy records.
	Project string `json:"project,omitempty"`
}
