//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	uuid "github.com/google/uuid"
)

// TestHeadlessKeepAlive: a keep-alive run reports its task turn, runs each
// run_agent_input frame as a further turn in the same session and exits once
// stdin closes.
func TestHeadlessKeepAlive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, binPath, "headless", "--keep-alive", "--session-id", "keeper-1", "-m", testModel, "say hello")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"INFER_GATEWAY_MOCK=true",
		"INFER_GATEWAY_MOCK_SCENARIOS="+filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml"),
		"INFER_STORAGE_ENABLED=false",
	)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())

	turns := make(chan map[string]any, 8)
	go func() {
		defer close(turns)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(nil, 4<<20)
		for scanner.Scan() {
			var line map[string]any
			if json.Unmarshal(scanner.Bytes(), &line) == nil && line["type"] == "subagent_turn" {
				turns <- line
			}
		}
	}()
	nextTurn := func() map[string]any {
		t.Helper()
		select {
		case turn, ok := <-turns:
			if !ok {
				t.Fatalf("stdout closed before the turn line\nstderr:\n%s", stderr.String())
			}
			return turn
		case <-time.After(45 * time.Second):
			t.Fatalf("no subagent_turn line within 45s\nstderr:\n%s", stderr.String())
			return nil
		}
	}

	first := nextTurn()
	require.Equal(t, "Hello! How can I help?", first["final_assistant"])
	require.Equal(t, true, first["done"])

	frame, err := json.Marshal(map[string]any{"type": "run_agent_input", "input": map[string]any{
		"messages": []map[string]any{{"id": uuid.NewString(), "role": "user", "content": "say hello once more"}},
	}})
	require.NoError(t, err)
	_, err = stdin.Write(append(frame, '\n'))
	require.NoError(t, err)

	second := nextTurn()
	require.Equal(t, "Hello! How can I help?", second["final_assistant"])
	require.Equal(t, true, second["success"])

	require.NoError(t, stdin.Close())
	if err := cmd.Wait(); err != nil {
		t.Fatalf("stdin EOF must end the keep-alive run cleanly: %v\nstderr:\n%s", err, stderr.String())
	}
}
