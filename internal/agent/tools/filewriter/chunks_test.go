package filewriter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

// setupChunkTest reuses the sandboxed writer from writer_test.go and returns a
// chunk manager streaming through a temp dir inside the sandbox.
func setupChunkTest(t *testing.T) (string, ChunkManager, context.Context) {
	tempDir, writer, ctx := setupWriterTest(t)
	return tempDir, NewStreamingChunkManager(filepath.Join(tempDir, "chunks"), writer), ctx
}

func TestStreamingChunkManager_FinalizeConcatenatesChunks(t *testing.T) {
	tempDir, cm, ctx := setupChunkTest(t)
	target := filepath.Join(tempDir, "out.txt")

	require.NoError(t, cm.WriteChunk(ctx, ChunkWriteRequest{SessionID: "s1", ChunkIndex: 0, Data: []byte("hello ")}))
	require.NoError(t, cm.WriteChunk(ctx, ChunkWriteRequest{SessionID: "s1", ChunkIndex: 1, Data: []byte("world"), IsLast: true}))

	result, err := cm.FinalizeChunks(ctx, "s1", target)
	require.NoError(t, err)
	require.Equal(t, target, result.Path)
	require.Equal(t, int64(len("hello world")), result.BytesWritten)

	content, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, "hello world", string(content))

	_, err = cm.GetSessionInfo("s1")
	require.ErrorContains(t, err, "session not found", "finalize must drop the session")
}

func TestStreamingChunkManager_OutOfOrderChunkRejected(t *testing.T) {
	_, cm, ctx := setupChunkTest(t)

	require.NoError(t, cm.WriteChunk(ctx, ChunkWriteRequest{SessionID: "s2", ChunkIndex: 0, Data: []byte("a")}))

	err := cm.WriteChunk(ctx, ChunkWriteRequest{SessionID: "s2", ChunkIndex: 2, Data: []byte("c")})
	require.ErrorContains(t, err, "chunk index mismatch: expected 1, got 2")
}

func TestStreamingChunkManager_IncompleteSessionRejected(t *testing.T) {
	tempDir, cm, ctx := setupChunkTest(t)

	require.NoError(t, cm.WriteChunk(ctx, ChunkWriteRequest{SessionID: "s3", ChunkIndex: 0, Data: []byte("a"), IsLast: true}))
	require.NoError(t, cm.WriteChunk(ctx, ChunkWriteRequest{SessionID: "s3", ChunkIndex: 1, Data: []byte("b")}))

	_, err := cm.FinalizeChunks(ctx, "s3", filepath.Join(tempDir, "never.txt"))
	require.ErrorContains(t, err, "incomplete session")
}

func TestStreamingChunkManager_SessionInfoAndCleanup(t *testing.T) {
	_, cm, ctx := setupChunkTest(t)

	_, err := cm.GetSessionInfo("unknown")
	require.ErrorContains(t, err, "session not found: unknown")

	require.NoError(t, cm.WriteChunk(ctx, ChunkWriteRequest{SessionID: "s4", ChunkIndex: 0, Data: []byte("x"), IsLast: true}))

	info, err := cm.GetSessionInfo("s4")
	require.NoError(t, err)
	require.Equal(t, "s4", info.SessionID)
	require.Equal(t, 1, info.TotalChunks)
	require.True(t, info.Created)

	require.NoError(t, cm.CleanupSession("s4"))
	_, err = cm.GetSessionInfo("s4")
	require.ErrorContains(t, err, "session not found")

	require.NoError(t, cm.CleanupSession("s4"), "cleaning up an unknown session is a no-op")
}

func TestStreamingChunkManager_FinalizeUnknownSession(t *testing.T) {
	tempDir, cm, ctx := setupChunkTest(t)
	_, err := cm.FinalizeChunks(ctx, "nope", filepath.Join(tempDir, "out.txt"))
	require.ErrorContains(t, err, "session not found: nope")
}
