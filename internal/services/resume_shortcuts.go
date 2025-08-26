package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ResumeShortcut resumes a previous conversation
type ResumeShortcut struct {
	persistentRepo *PersistentConversationRepository
}

// NewResumeShortcut creates a new resume shortcut
func NewResumeShortcut(persistentRepo *PersistentConversationRepository) *ResumeShortcut {
	return &ResumeShortcut{
		persistentRepo: persistentRepo,
	}
}

func (r *ResumeShortcut) GetName() string        { return "resume" }
func (r *ResumeShortcut) GetDescription() string { return "Resume a previous conversation" }
func (r *ResumeShortcut) GetUsage() string       { return "/resume [conversation_id|list|<number>]" }
func (r *ResumeShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

// ShortcutResult represents the result of a shortcut execution
type ShortcutResult struct {
	Output     string
	Success    bool
	SideEffect string
	Data       interface{}
}

func (r *ResumeShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) == 0 {
		// Show recent conversations for selection
		return r.listRecentConversations(ctx)
	}

	arg := args[0]

	// Handle "list" command
	if arg == "list" {
		return r.listRecentConversations(ctx)
	}

	// Try to parse as conversation number (1-based index)
	if num, err := strconv.Atoi(arg); err == nil && num > 0 {
		return r.resumeByIndex(ctx, num-1) // Convert to 0-based index
	}

	// Treat as conversation ID
	return r.resumeByID(ctx, arg)
}

// listRecentConversations shows a list of recent conversations
func (r *ResumeShortcut) listRecentConversations(ctx context.Context) (ShortcutResult, error) {
	conversations, err := r.persistentRepo.ListSavedConversations(ctx, 10, 0)
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
		// Truncate title if too long
		title := conv.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}

		output.WriteString(fmt.Sprintf("**%d.** %s\n", i+1, title))
		output.WriteString(fmt.Sprintf("   📅 %s • 💬 %d messages • 🔤 %d tokens\n", 
			conv.UpdatedAt.Format("Jan 2, 2006 3:04 PM"), 
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

// resumeByIndex resumes a conversation by its index in the recent list
func (r *ResumeShortcut) resumeByIndex(ctx context.Context, index int) (ShortcutResult, error) {
	conversations, err := r.persistentRepo.ListSavedConversations(ctx, 10, 0)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to list conversations: %v", err),
			Success: false,
		}, nil
	}

	if index >= len(conversations) {
		return ShortcutResult{
			Output:  fmt.Sprintf("Invalid conversation number. Use `/resume list` to see available conversations."),
			Success: false,
		}, nil
	}

	conv := conversations[index]
	return r.resumeByID(ctx, conv.ID)
}

// resumeByID resumes a conversation by its ID
func (r *ResumeShortcut) resumeByID(ctx context.Context, conversationID string) (ShortcutResult, error) {
	if err := r.persistentRepo.LoadConversation(ctx, conversationID); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to resume conversation: %v", err),
			Success: false,
		}, nil
	}

	metadata := r.persistentRepo.GetCurrentConversationMetadata()
	
	return ShortcutResult{
		Output: fmt.Sprintf("🔄 Resumed conversation: **%s**\n"+
			"📅 Last updated: %s\n"+
			"💬 Messages: %d • 🔤 Tokens: %d\n"+
			"📋 ID: `%s`",
			metadata.Title,
			metadata.UpdatedAt.Format("Jan 2, 2006 3:04 PM"),
			metadata.MessageCount,
			metadata.TokenStats.TotalTokens,
			metadata.ID),
		Success:    true,
		SideEffect: "resume_conversation",
		Data:       conversationID,
	}, nil
}

// SaveShortcut saves the current conversation
type SaveShortcut struct {
	persistentRepo *PersistentConversationRepository
}

// NewSaveShortcut creates a new save shortcut
func NewSaveShortcut(persistentRepo *PersistentConversationRepository) *SaveShortcut {
	return &SaveShortcut{
		persistentRepo: persistentRepo,
	}
}

func (s *SaveShortcut) GetName() string               { return "save" }
func (s *SaveShortcut) GetDescription() string        { return "Save current conversation" }
func (s *SaveShortcut) GetUsage() string              { return "/save [title]" }
func (s *SaveShortcut) CanExecute(args []string) bool { return len(args) <= 10 } // Allow multi-word titles

func (s *SaveShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	currentID := s.persistentRepo.GetCurrentConversationID()
	
	// If no active conversation, start a new one
	if currentID == "" {
		title := "New Conversation"
		if len(args) > 0 {
			title = strings.Join(args, " ")
		}
		
		if err := s.persistentRepo.StartNewConversation(title); err != nil {
			return ShortcutResult{
				Output:  fmt.Sprintf("Failed to start new conversation: %v", err),
				Success: false,
			}, nil
		}
		
		currentID = s.persistentRepo.GetCurrentConversationID()
	}

	// Update title if provided
	if len(args) > 0 {
		title := strings.Join(args, " ")
		s.persistentRepo.SetConversationTitle(title)
	}

	// Save the conversation
	if err := s.persistentRepo.SaveConversation(ctx); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to save conversation: %v", err),
			Success: false,
		}, nil
	}

	metadata := s.persistentRepo.GetCurrentConversationMetadata()

	return ShortcutResult{
		Output: fmt.Sprintf("💾 Conversation saved: **%s**\n"+
			"💬 Messages: %d • 🔤 Tokens: %d\n"+
			"📋 ID: `%s`",
			metadata.Title,
			metadata.MessageCount,
			metadata.TokenStats.TotalTokens,
			metadata.ID),
		Success: true,
	}, nil
}