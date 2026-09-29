//go:build e2e

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"
)

// serveWorker drives an `infer headless --serve` process line by line: frames go
// in on stdin, AG-UI events and browser commands come out on stdout.
type serveWorker struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *bytes.Buffer
	lines  chan map[string]any
}

func startServeWorker(t *testing.T, env ...string) *serveWorker {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, binPath, "headless", "--serve", "--mode", "auto", "--session-id", "worker-1", "-m", testModel)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"INFER_GATEWAY_MOCK=true",
		"INFER_GATEWAY_MOCK_SCENARIOS="+filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml"),
		"INFER_STORAGE_ENABLED=false",
	)
	cmd.Env = append(cmd.Env, env...)

	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	w := &serveWorker{t: t, cmd: cmd, stdin: stdin, stderr: &bytes.Buffer{}, lines: make(chan map[string]any, 256)}
	cmd.Stderr = w.stderr
	require.NoError(t, cmd.Start())

	go func() {
		defer close(w.lines)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(nil, 4<<20)
		for scanner.Scan() {
			var line map[string]any
			if json.Unmarshal(scanner.Bytes(), &line) == nil {
				w.lines <- line
			}
		}
	}()
	return w
}

func (w *serveWorker) send(frame map[string]any) {
	w.t.Helper()
	data, err := json.Marshal(frame)
	require.NoError(w.t, err)
	_, err = w.stdin.Write(append(data, '\n'))
	require.NoError(w.t, err)
}

// run sends one user_message and collects the run it produces, from RUN_STARTED
// through its terminal event. react sees every line on the way, so a test can
// answer a browser command or interrupt the turn mid-stream.
func (w *serveWorker) run(message string, react func(line map[string]any)) []map[string]any {
	w.t.Helper()
	w.send(map[string]any{"type": "user_message", "content": message})
	var events []map[string]any
	timeout := time.After(45 * time.Second)
	for {
		select {
		case line, ok := <-w.lines:
			require.True(w.t, ok, "worker stdout closed mid-run\nstderr:\n%s", w.stderr)
			if react != nil {
				react(line)
			}
			if line["type"] == "browser_command" {
				continue
			}
			events = append(events, line)
			if line["type"] == "RUN_FINISHED" || line["type"] == "RUN_ERROR" {
				return events
			}
		case <-timeout:
			w.t.Fatalf("timed out waiting for the run of %q\nevents: %v\nstderr:\n%s", message, events, w.stderr)
		}
	}
}

func requireOneRun(t *testing.T, events []map[string]any, wantOutcome string) {
	t.Helper()
	require.Equal(t, "RUN_STARTED", events[0]["type"], "a turn must open with RUN_STARTED: %v", events)
	require.Equal(t, "worker-1", events[0]["threadId"])
	terminal := events[len(events)-1]
	require.Equal(t, "RUN_FINISHED", terminal["type"], "events: %v", events)
	require.Equal(t, map[string]any{"type": wantOutcome}, terminal["outcome"])
	for _, ev := range events[1 : len(events)-1] {
		require.NotContains(t, []any{"RUN_STARTED", "RUN_FINISHED", "RUN_ERROR"}, ev["type"], "a turn must be exactly one run: %v", events)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func TestHeadlessServeWorker(t *testing.T) {
	extensionPort := freePort(t)
	w := startServeWorker(t,
		"INFER_BROWSER_USE_ENABLED=true",
		"INFER_BROWSER_USE_BACKEND=extension",
		"INFER_BROWSER_USE_EXTENSION_PORT="+strconv.Itoa(extensionPort),
	)

	requireOneRun(t, w.run("say hello", nil), "success")

	_, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", extensionPort), time.Second)
	require.Error(t, err, "a serve worker must not bind the extension bridge port")

	var command map[string]any
	browserRun := w.run("open the example page", func(line map[string]any) {
		if line["type"] != "browser_command" {
			return
		}
		command = line
		w.send(map[string]any{"type": "browser_result", "id": line["id"], "url": "https://example.com/", "title": "Example Domain"})
	})
	requireOneRun(t, browserRun, "success")
	require.Equal(t, "navigate", command["action"])
	require.Equal(t, "https://example.com", command["url"])
	require.Contains(t, statusOfType(browserRun, "TOOL_CALL_RESULT")["content"], "Example Domain")

	interrupted := false
	requireOneRun(t, w.run("tell me a long story", func(line map[string]any) {
		if line["type"] == "TEXT_MESSAGE_CONTENT" && !interrupted {
			interrupted = true
			w.send(map[string]any{"type": "interrupt"})
		}
	}), "cancelled")

	require.NoError(t, w.stdin.Close())
	require.NoError(t, w.cmd.Wait(), "stdin EOF must shut the worker down cleanly\nstderr:\n%s", w.stderr)
}
