//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	uuid "github.com/google/uuid"

	sdk "github.com/inference-gateway/sdk"
	tokenless "github.com/inference-gateway/tokenless"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// writeShowFixture stores one conversation of 25 entries under dir through the
// real JSONL writer. Even-numbered entries mention "marker", so 13 of the 25
// entries match a --grep marker and interleave with the 12 fillers - and the
// matches sit at session indexes that a raw-session append window would never
// touch (the 11th match is session entry 20, not session entry 11).
func writeShowFixture(t *testing.T, dir string) string {
	t.Helper()

	id := uuid.NewString()
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	entries := make([]convdomain.ConversationEntry, 0, 25)
	for i := range 25 {
		text := fmt.Sprintf("entry-%02d just filler text", i)
		if i%2 == 0 {
			text = fmt.Sprintf("entry-%02d mentions the marker", i)
		}
		role := sdk.User
		if i%2 == 1 {
			role = sdk.Assistant
		}
		entries = append(entries, convdomain.ConversationEntry{
			Message: sdk.Message{Role: role, Content: sdk.NewMessageContent(text)},
			Time:    base.Add(time.Duration(i) * time.Second),
		})
	}

	store, err := storage.NewJsonlStorage(storage.JsonlStorageConfig{Path: dir})
	require.NoError(t, err)
	meta := convdomain.ConversationMetadata{
		ID:           id,
		Title:        "show window e2e",
		CreatedAt:    base,
		UpdatedAt:    base.Add(25 * time.Second),
		MessageCount: len(entries),
	}
	require.NoError(t, store.SaveConversation(context.Background(), id, entries, meta))
	return id
}

// appendStartsFromGrepResults drives the built binary against a JSONL store and
// verifies that --append pages the --grep RESULTS: it skips the first 10
// matches and picks up at the 11th, not at session entry 11.
func appendStartsFromGrepResults(t *testing.T) {
	m := startMock(t)
	store := filepath.Join(t.TempDir(), "conversations")
	id := writeShowFixture(t, store)

	env := inferEnv(m.URL)
	delete(env, "INFER_STORAGE_ENABLED")
	env["HOME"] = t.TempDir()
	env["INFER_STORAGE_ENABLED"] = "true"
	env["INFER_STORAGE_TYPE"] = "jsonl"
	env["INFER_STORAGE_JSONL_PATH"] = store

	runShow := func(args ...string) (string, int) {
		t.Helper()
		res := tokenless.Orchestrator{Bin: binPath, Dir: t.TempDir(), Stdin: "", Env: env}.Run(t, args...)
		if res.ExitCode != 0 {
			t.Fatalf("conversations show %v failed (%d)\nstderr:\n%s", args, res.ExitCode, res.Stderr)
		}
		return res.Stdout, res.ExitCode
	}
	textRun := func(args ...string) string {
		t.Helper()
		stdout, code := runShow(args...)
		if code != 0 {
			t.Fatalf("conversations show %v failed (%d)", args, code)
		}
		return stdout
	}

	head := decodeShowOutput(t, textRun("conversations", "show", id, "--format", "json"))
	require.Len(t, head.Entries, 10)
	require.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, contentIndexes(t, head))
	require.True(t, head.Truncated)

	grepped := decodeShowOutput(t, textRun("conversations", "show", id, "--grep", "marker", "--format", "json"))
	require.Len(t, grepped.Entries, 10)
	require.Equal(t, []int{0, 2, 4, 6, 8, 10, 12, 14, 16, 18}, contentIndexes(t, grepped))
	for _, e := range grepped.Entries {
		require.Contains(t, e.Content, "marker")
	}
	require.True(t, grepped.Truncated)

	appended := decodeShowOutput(t, textRun("conversations", "show", id, "--grep", "marker", "--append", "--format", "json"))
	require.Len(t, appended.Entries, 3)
	require.Equal(t, []int{20, 22, 24}, contentIndexes(t, appended))
	require.True(t, appended.Truncated)

	shorthand := decodeShowOutput(t, textRun("conversations", "show", id, "--grep", "marker", "-a", "--format", "json"))
	require.Equal(t, []int{20, 22, 24}, contentIndexes(t, shorthand))

	plain := decodeShowOutput(t, textRun("conversations", "show", id, "--append", "--format", "json"))
	require.Len(t, plain.Entries, 15)
	require.Equal(t, []int{10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24}, contentIndexes(t, plain))

	tail := textRun("conversations", "show", id, "--grep", "marker", "--append")
	require.Contains(t, tail, "#11 [user]")
	require.Contains(t, tail, "Skipped the first 10 entries")

	headText := textRun("conversations", "show", id, "--grep", "marker")
	require.Contains(t, headText, "Showing the first 10 of 13 entries")
	require.Contains(t, headText, "#1 [user]")
}

type showJSON struct {
	Entries []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"entries"`
	Truncated bool `json:"truncated"`
}

func decodeShowOutput(t *testing.T, stdout string) showJSON {
	t.Helper()
	var out showJSON
	require.NoError(t, json.Unmarshal([]byte(stdout), &out), "stdout:\n%s", stdout)
	return out
}

// contentIndexes reads the session index each rendered entry came from: the
// fixture names every entry "entry-NN ...".
func contentIndexes(t *testing.T, out showJSON) []int {
	t.Helper()
	indexes := make([]int, 0, len(out.Entries))
	for _, e := range out.Entries {
		var i int
		_, err := fmt.Sscanf(e.Content, "entry-%2d", &i)
		require.NoError(t, err, "content %q", e.Content)
		indexes = append(indexes, i)
	}
	return indexes
}

func TestConversationsShowAppendStartsFromGrepResults(t *testing.T) {
	t.Run("append windows the grep results", appendStartsFromGrepResults)
}
