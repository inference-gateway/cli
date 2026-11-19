package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func newProjectStore(t *testing.T, projectsPath, project string) *JsonlStorage {
	t.Helper()
	store, err := NewJsonlStorage(JsonlStorageConfig{
		Path:         filepath.Join(projectsPath, config.ProjectSlug(project), "conversations"),
		ProjectsPath: projectsPath,
	})
	require.NoError(t, err)
	return store
}

func saveInProject(t *testing.T, store ConversationStorage, id, project string) {
	t.Helper()
	metadata := createTestMetadata(id)
	metadata.Project = project
	require.NoError(t, store.SaveConversation(context.Background(), id, createTestEntries(), metadata))
}

func TestMoveConversation_JsonlMovesTheFileToTheProjectsStore(t *testing.T) {
	ctx := context.Background()
	projectsPath := t.TempDir()
	from, to := "/home/alice/proj-a", "/home/alice/proj-b"
	source := newProjectStore(t, projectsPath, from)
	saveInProject(t, source, "conv", from)

	require.NoError(t, MoveConversation(ctx, source, "conv", to))

	assert.NoFileExists(t, filepath.Join(projectsPath, config.ProjectSlug(from), "conversations", "conv.jsonl"))
	entries, metadata, err := newProjectStore(t, projectsPath, to).LoadConversation(ctx, "conv")
	require.NoError(t, err)
	assert.Equal(t, to, metadata.Project)
	assert.Len(t, entries, len(createTestEntries()))

	listed, err := source.ListConversations(ctx, "", 0, 0)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, to, listed[0].Project, "the listing across projects names the store that holds it")
}

func TestMoveConversation_JsonlWithinItsStoreRewritesTheProject(t *testing.T) {
	ctx := context.Background()
	projectsPath := t.TempDir()
	project := "/home/alice/proj-a"
	store := newProjectStore(t, projectsPath, project)
	saveInProject(t, store, "conv", "/home/alice/stale")

	require.NoError(t, MoveConversation(ctx, store, "conv", project))

	_, metadata, err := store.LoadConversation(ctx, "conv")
	require.NoError(t, err)
	assert.Equal(t, project, metadata.Project)
}

func TestMoveConversation_JsonlRefusesToOverwriteTheTargetsConversation(t *testing.T) {
	ctx := context.Background()
	projectsPath := t.TempDir()
	from, to := "/home/alice/proj-a", "/home/alice/proj-b"
	source := newProjectStore(t, projectsPath, from)
	saveInProject(t, source, "conv", from)
	saveInProject(t, newProjectStore(t, projectsPath, to), "conv", to)

	require.Error(t, MoveConversation(ctx, source, "conv", to))

	_, err := os.Stat(filepath.Join(projectsPath, config.ProjectSlug(from), "conversations", "conv.jsonl"))
	assert.NoError(t, err, "the source stays when the move is refused")
}

func TestMoveConversation_SharedStoreRewritesTheProject(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStorage()
	saveInProject(t, store, "conv", "/home/alice/proj-a")

	require.NoError(t, MoveConversation(ctx, store, "conv", "/home/alice/proj-b"))

	entries, metadata, err := store.LoadConversation(ctx, "conv")
	require.NoError(t, err)
	assert.Equal(t, "/home/alice/proj-b", metadata.Project)
	assert.Len(t, entries, len(createTestEntries()))
}

func TestMoveConversation_UnknownConversationIsNotFound(t *testing.T) {
	ctx := context.Background()
	stores := map[string]ConversationStorage{
		"jsonl":  newProjectStore(t, t.TempDir(), "/home/alice/proj-a"),
		"memory": NewMemoryStorage(),
	}
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			err := MoveConversation(ctx, store, "missing", "/home/alice/proj-b")
			assert.ErrorIs(t, err, convdomain.ErrConversationNotFound)
		})
	}
}
