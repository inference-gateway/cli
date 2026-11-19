package storage

import (
	"context"
)

// conversationMover is a store that keeps a directory per project, so moving a
// conversation to another project moves where it is kept.
type conversationMover interface {
	MoveConversation(ctx context.Context, conversationID, dir string) error
}

// MoveConversation records the project at dir as a conversation's project. A
// store with a directory per project also moves the conversation there. Every
// other store keeps all projects' conversations together, so it saves the
// conversation again with the new project.
func MoveConversation(ctx context.Context, store ConversationStorage, conversationID, dir string) error {
	if mover, ok := store.(conversationMover); ok {
		return mover.MoveConversation(ctx, conversationID, dir)
	}
	entries, metadata, err := store.LoadConversation(ctx, conversationID)
	if err != nil {
		return err
	}
	metadata.Project = dir
	return store.SaveConversation(ctx, conversationID, entries, metadata)
}
