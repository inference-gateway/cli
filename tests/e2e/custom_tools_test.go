//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	tokenless "github.com/inference-gateway/tokenless"
	mockgateway "github.com/inference-gateway/tokenless/gateway"
)

// TestCustomToolsExample runs examples/custom-tools against the model its
// scenarios.yaml scripts, so the example's README stays true.
func TestCustomToolsExample(t *testing.T) {
	example := filepath.Join(repoRoot(), "examples", "custom-tools")
	defs, err := mockgateway.LoadFile(filepath.Join(example, "scenarios.yaml"))
	require.NoError(t, err)
	m := tokenless.StartMock(t, defs)

	toolResult := func(t *testing.T, dir string, args ...string) string {
		t.Helper()
		env := inferEnv(m.URL)
		env["INFER_TOOLS_CUSTOM_DIR"] = filepath.Join(example, "tools")
		env["INFER_AGENT_MODEL"] = "mock/openai/gpt-4o"
		res := tokenless.Orchestrator{Bin: binPath, Dir: dir, Env: env}.Run(t, append([]string{"headless"}, args...)...)
		require.Zero(t, res.ExitCode, "stderr:\n%s", res.Stderr)
		toolResults := contentsByRole(jsonLines(t, res.Stdout), "tool")
		require.Len(t, toolResults, 1)
		return toolResults[0]
	}

	t.Run("WordCount runs without approval", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("one two\nthree\n"), 0o600))
		require.Contains(t, toolResult(t, dir, "count the words in README.md"), "README.md: 2 lines, 3 words, 14 characters")
	})

	t.Run("SaveNote is blocked without an approver", func(t *testing.T) {
		dir := t.TempDir()
		require.Contains(t, toolResult(t, dir, "save a note to buy milk"), "Blocked: SaveNote requires approval")
		require.NoFileExists(t, filepath.Join(dir, "notes.jsonl"))
	})

	t.Run("SaveNote runs in auto mode", func(t *testing.T) {
		dir := t.TempDir()
		require.Contains(t, toolResult(t, dir, "--mode", "auto", "save a note to buy milk"), "Saved the note to notes.jsonl.")
		notes, err := os.ReadFile(filepath.Join(dir, "notes.jsonl"))
		require.NoError(t, err)
		require.JSONEq(t, `{"text":"Buy milk"}`, string(notes))
	})

	t.Run("SaveNote is refused in plan mode", func(t *testing.T) {
		dir := t.TempDir()
		require.Contains(t, toolResult(t, dir, "--mode", "plan", "save a note to buy milk"), "tool not allowed: SaveNote")
		require.NoFileExists(t, filepath.Join(dir, "notes.jsonl"))
	})
}
