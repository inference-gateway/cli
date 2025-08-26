package shortcuts

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// PersistentConversationRepository interface for conversation persistence
type PersistentConversationRepository interface {
	ListSavedConversations(ctx context.Context, limit, offset int) ([]ConversationSummary, error)
	LoadConversation(ctx context.Context, conversationID string) error
	GetCurrentConversationMetadata() ConversationMetadata
	SaveConversation(ctx context.Context) error
	StartNewConversation(title string) error
	GetCurrentConversationID() string
	SetConversationTitle(title string)
}

// ConversationSummary represents a saved conversation summary
type ConversationSummary struct {
	ID           string
	Title        string
	CreatedAt    string
	UpdatedAt    string
	MessageCount int
	TokenStats   TokenStats
	Model        string
	Tags         []string
	Summary      string
}

// ConversationMetadata represents conversation metadata
type ConversationMetadata struct {
	ID           string
	Title        string
	CreatedAt    string
	UpdatedAt    string
	MessageCount int
	TokenStats   TokenStats
	Model        string
	Tags         []string
	Summary      string
}

// TokenStats represents token usage statistics
type TokenStats struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalTokens       int
	RequestCount      int
}

// ResumeShortcut resumes a previous conversation
type ResumeShortcut struct {
	repo PersistentConversationRepository
}

// NewResumeShortcut creates a new resume shortcut
func NewResumeShortcut(repo PersistentConversationRepository) *ResumeShortcut {
	return &ResumeShortcut{repo: repo}
}

func (r *ResumeShortcut) GetName() string               { return "resume" }
func (r *ResumeShortcut) GetDescription() string        { return "Resume a previous conversation" }
func (r *ResumeShortcut) GetUsage() string              { return "/resume [conversation_id|list|<number>]" }
func (r *ResumeShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (r *ResumeShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		return r.listRecentConversations(ctx)
	}

	arg := args[0]

	if arg == "list" {
		return r.listRecentConversations(ctx)
	}

	if num, err := strconv.Atoi(arg); err == nil && num > 0 {
		return r.resumeByIndex(ctx, num-1)
	}

	return r.resumeByID(ctx, arg)
}

func (r *ResumeShortcut) listRecentConversations(ctx context.Context) (ShortcutResult, error) {
	conversations, err := r.repo.ListSavedConversations(ctx, 10, 0)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to list conversations: %v", err),
			Success: false,
		}, nil
	}

	if len(conversations) == 0 {
		return ShortcutResult{
			Output:  "📝 No saved conversations found. Start chatting to create your first conversation!",
			Success: true,
		}, nil
	}

	var output strings.Builder
	output.WriteString("## Recent Conversations\n\n")

	for i, conv := range conversations {
		title := conv.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}

		output.WriteString(fmt.Sprintf("**%d.** %s\n", i+1, title))
		output.WriteString(fmt.Sprintf("   📅 %s • 💬 %d messages • 🔤 %d tokens\n",
			conv.UpdatedAt,
			conv.MessageCount,
			conv.TokenStats.TotalTokens))

		if conv.Model != "" {
			output.WriteString(fmt.Sprintf("   🤖 %s", conv.Model))
		}

		if len(conv.Tags) > 0 {
			output.WriteString(fmt.Sprintf(" • 🏷️ %s", strings.Join(conv.Tags, ", ")))
		}

		output.WriteString(fmt.Sprintf("\n   📋 ID: `%s`\n\n", conv.ID))
	}

	output.WriteString("💡 *Use `/resume <number>` or `/resume <id>` to resume a conversation.*")

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}

func (r *ResumeShortcut) resumeByIndex(ctx context.Context, index int) (ShortcutResult, error) {
	conversations, err := r.repo.ListSavedConversations(ctx, 10, 0)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to list conversations: %v", err),
			Success: false,
		}, nil
	}

	if index >= len(conversations) {
		return ShortcutResult{
			Output:  "Invalid conversation number. Use `/resume list` to see available conversations.",
			Success: false,
		}, nil
	}

	conv := conversations[index]
	return r.resumeByID(ctx, conv.ID)
}

func (r *ResumeShortcut) resumeByID(ctx context.Context, conversationID string) (ShortcutResult, error) {
	if err := r.repo.LoadConversation(ctx, conversationID); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to resume conversation: %v", err),
			Success: false,
		}, nil
	}

	metadata := r.repo.GetCurrentConversationMetadata()

	return ShortcutResult{
		Output: fmt.Sprintf("🔄 Resumed conversation: **%s**\n"+
			"📅 Last updated: %s\n"+
			"💬 Messages: %d • 🔤 Tokens: %d\n"+
			"📋 ID: `%s`",
			metadata.Title,
			metadata.UpdatedAt,
			metadata.MessageCount,
			metadata.TokenStats.TotalTokens,
			metadata.ID),
		Success:    true,
		SideEffect: SideEffectResumeConversation,
		Data:       conversationID,
	}, nil
}

// SaveShortcut saves the current conversation
type SaveShortcut struct {
	repo PersistentConversationRepository
}

// NewSaveShortcut creates a new save shortcut
func NewSaveShortcut(repo PersistentConversationRepository) *SaveShortcut {
	return &SaveShortcut{repo: repo}
}

func (s *SaveShortcut) GetName() string               { return "save" }
func (s *SaveShortcut) GetDescription() string        { return "Save current conversation" }
func (s *SaveShortcut) GetUsage() string              { return "/save [title]" }
func (s *SaveShortcut) CanExecute(args []string) bool { return len(args) <= 10 }

func (s *SaveShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	currentID := s.repo.GetCurrentConversationID()

	if currentID == "" {
		title := "New Conversation"
		if len(args) > 0 {
			title = strings.Join(args, " ")
		}

		if err := s.repo.StartNewConversation(title); err != nil {
			return ShortcutResult{
				Output:  fmt.Sprintf("Failed to start new conversation: %v", err),
				Success: false,
			}, nil
		}
	}

	if len(args) > 0 {
		title := strings.Join(args, " ")
		s.repo.SetConversationTitle(title)
	}

	if err := s.repo.SaveConversation(ctx); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to save conversation: %v", err),
			Success: false,
		}, nil
	}

	metadata := s.repo.GetCurrentConversationMetadata()

	return ShortcutResult{
		Output: fmt.Sprintf("💾 Conversation saved: **%s**\n"+
			"💬 Messages: %d • 🔤 Tokens: %d\n"+
			"📋 ID: `%s`",
			metadata.Title,
			metadata.MessageCount,
			metadata.TokenStats.TotalTokens,
			metadata.ID),
		Success:    true,
		SideEffect: SideEffectSaveConversation,
	}, nil
}