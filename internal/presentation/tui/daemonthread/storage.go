package daemonthread

import (
	"context"
	"encoding/json"
	"errors"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// errDeleteRemote refuses deleting a conversation the daemon's worker stores.
var errDeleteRemote = errors.New("conversations are deleted from the daemon's own storage, not from a daemon chat")

// Storage is the chat TUI's conversation storage in daemon mode: the thread's
// worker stores every conversation, and the TUI reads them over the binding.
// Writes are the worker's own, so saving here does nothing.
type Storage struct {
	thread *Thread
}

// NewStorage builds the daemon-mode conversation storage over thread.
func NewStorage(thread *Thread) *Storage {
	return &Storage{thread: thread}
}

// LoadConversation follows the conversation's thread and reads the
// conversation as its worker stored it.
func (s *Storage) LoadConversation(ctx context.Context, conversationID string) ([]convdomain.ConversationEntry, convdomain.ConversationMetadata, error) {
	if !s.thread.Follows(conversationID) {
		if err := s.thread.Follow(ctx, conversationID, sessionsdomain.ThreadOptions{}); err != nil {
			return nil, convdomain.ConversationMetadata{}, err
		}
	}
	frame := map[string]string{"type": "export_conversation", "id": conversationID, "project_dir": s.thread.opts.ProjectDir}
	data, err := s.thread.Request(ctx, frame, "conversation_export")
	if err != nil {
		return nil, convdomain.ConversationMetadata{}, err
	}
	var exported struct {
		Entries  []convdomain.ConversationEntry  `json:"entries"`
		Metadata convdomain.ConversationMetadata `json:"metadata"`
		Error    string                          `json:"error"`
	}
	if err := json.Unmarshal(data, &exported); err != nil {
		return nil, convdomain.ConversationMetadata{}, err
	}
	if exported.Error != "" {
		return nil, convdomain.ConversationMetadata{}, errors.New(exported.Error)
	}
	s.thread.SetSynced(conversationID, len(exported.Entries))
	return exported.Entries, exported.Metadata, nil
}

// ListConversations lists the conversations of the environment the session
// runs on, in the worker's project.
func (s *Storage) ListConversations(ctx context.Context, _ string, _, offset int) ([]convdomain.ConversationSummary, error) {
	if offset > 0 {
		return nil, nil
	}
	data, err := s.thread.Request(ctx, map[string]string{"type": "list_conversations", "project_dir": s.thread.opts.ProjectDir}, "conversations")
	if err != nil {
		return nil, err
	}
	var listed struct {
		Conversations []convdomain.ConversationSummary `json:"conversations"`
	}
	if err := json.Unmarshal(data, &listed); err != nil {
		return nil, err
	}
	return listed.Conversations, nil
}

func (s *Storage) SaveConversation(context.Context, string, []convdomain.ConversationEntry, convdomain.ConversationMetadata) error {
	return nil
}

func (s *Storage) DeleteConversation(context.Context, string) error {
	return errDeleteRemote
}

func (s *Storage) UpdateConversationMetadata(context.Context, string, convdomain.ConversationMetadata) error {
	return nil
}

func (s *Storage) ListConversationsNeedingTitles(context.Context, int) ([]convdomain.ConversationSummary, error) {
	return nil, nil
}

func (s *Storage) Close() error {
	return nil
}

// Health reports whether the daemon connection is up.
func (s *Storage) Health(context.Context) error {
	if s.thread.Placement().Offline {
		return ErrOffline
	}
	return nil
}
