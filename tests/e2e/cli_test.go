//go:build e2e

// Package e2e runs the real `infer` binary as a subprocess against an
// in-test mock inference-gateway (internal/mockgateway), asserting on the
// headless JSON output contract, the piped-chat streaming path, tool side
// effects on disk, and the exact requests received by the gateway.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"
	"github.com/stretchr/testify/require"

	mockgateway "github.com/inference-gateway/cli/internal/mockgateway"
)

var binPath string

// TestMain builds the CLI once for all tests. Set INFER_E2E_BINARY to reuse
// an already-built binary (e.g. the Taskfile output) and skip the build.
func TestMain(m *testing.M) {
	if p := os.Getenv("INFER_E2E_BINARY"); p != "" {
		binPath = p
		os.Exit(m.Run())
	}

	dir, err := os.MkdirTemp("", "infer-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "infer")

	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = repoRoot()
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building infer: %v\n%s", err, out)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func startMock(t *testing.T) (*mockgateway.Server, string) {
	t.Helper()
	gw := mockgateway.New(mockgateway.Default())
	ts := httptest.NewServer(gw)
	t.Cleanup(ts.Close)
	return gw, ts.URL
}

// runCLI executes the built binary with a hermetic environment: gateway URL
// pointed at the mock, gateway auto-start off, storage off, retry backoff
// zeroed, and HOME moved to a temp dir so ~/.infer never leaks in. Note
// INFER_AGENT_MODEL resolves through the resolveViperEnvironmentVariables
// backstop in cmd/config.go (agent.model has a zero default, so it is not a
// registered viper default).
func runCLI(t *testing.T, gatewayURL, dir, stdin string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"INFER_GATEWAY_URL="+gatewayURL,
		"INFER_GATEWAY_RUN=false",
		"INFER_AGENT_MODEL="+mockgateway.DefaultModel,
		"INFER_STORAGE_ENABLED=false",
		"INFER_CLIENT_RETRY_INITIAL_BACKOFF_SEC=0",
		"HOME="+t.TempDir(),
	)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	require.NoError(t, ctx.Err(), "CLI run exceeded the test timeout; stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "unexpected non-exit error: %v; stderr:\n%s", err, stderr.String())
		exitCode = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), exitCode
}

func runAgent(t *testing.T, gatewayURL, dir, prompt string) (string, int) {
	t.Helper()
	stdout, _, code := runCLI(t, gatewayURL, dir, "", "agent", prompt)
	return stdout, code
}

// jsonLines parses every stdout line into a generic map; the headless agent
// emits newline-delimited JSON only.
func jsonLines(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var obj map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &obj), "non-JSON stdout line: %q", line)
		lines = append(lines, obj)
	}
	return lines
}

func contentsByRole(lines []map[string]any, role string) []string {
	var out []string
	for _, l := range lines {
		if l["role"] == role {
			content, _ := l["content"].(string)
			out = append(out, content)
		}
	}
	return out
}

func statusOfType(lines []map[string]any, typ string) map[string]any {
	for _, l := range lines {
		if l["type"] == typ {
			return l
		}
	}
	return nil
}

func writeFixtures(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("fixture content\n"), 0o600))
	}
}

func toolMessages(body sdk.CreateChatCompletionRequest) []sdk.Message {
	var out []sdk.Message
	for _, m := range body.Messages {
		if m.Role == sdk.Tool {
			out = append(out, m)
		}
	}
	return out
}

func TestAgentTextOnlyTerminatesAfterTwoTurns(t *testing.T) {
	gw, url := startMock(t)

	stdout, code := runAgent(t, url, t.TempDir(), "say hello")
	require.Zero(t, code)

	lines := jsonLines(t, stdout)
	assistants := contentsByRole(lines, "assistant")
	require.Contains(t, assistants, "Hello! How can I help?")

	stats := statusOfType(lines, "session_stats")
	require.NotNil(t, stats, "a session_stats line must be emitted")
	require.EqualValues(t, 2, stats["requests"], "two consecutive no-tool-call turns end the run")
	require.EqualValues(t, 30, stats["total_tokens"], "usage must accumulate across both turns")

	reqs := gw.Requests()
	require.Len(t, reqs, 2)
	for _, r := range reqs {
		require.Equal(t, "text-only", r.Scenario, "the injected automated-check reminder must not re-route the scenario")
		require.False(t, r.Stream, "headless agent uses the non-streaming path")
	}
}

func TestAgentMockModeNeedsOnlyOneEnvVar(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "agent", "-m", mockgateway.DefaultModel, "say hello")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"INFER_GATEWAY_MOCK=true",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "stderr:\n%s", stderr.String())

	assistants := contentsByRole(jsonLines(t, stdout.String()), "assistant")
	require.Contains(t, assistants, "Hello! How can I help?")
}

func TestAgentParallelReadsExecuteAndReturnInOrder(t *testing.T) {
	gw, url := startMock(t)
	dir := t.TempDir()
	writeFixtures(t, dir, "a.txt", "b.txt", "c.txt", "d.txt")

	stdout, code := runAgent(t, url, dir, "please execute the Read tool 4 times in parallel")
	require.Zero(t, code)

	lines := jsonLines(t, stdout)
	toolResults := contentsByRole(lines, "tool")
	require.Len(t, toolResults, 4, "all four Read executions must be reported")
	for _, content := range toolResults {
		require.Contains(t, content, "Result of tool call", "Read is auto-approved and must succeed")
	}
	require.Contains(t, contentsByRole(lines, "assistant"), "All four files read.")

	reqs := gw.Requests()
	require.Len(t, reqs, 3, "tool turn, answer turn, then the automated-check turn")

	tools := toolMessages(reqs[1].Body)
	require.Len(t, tools, 4)
	for i, m := range tools {
		require.Equal(t, fmt.Sprintf("call_0_%d", i), *m.ToolCallID, "tool results must keep the original call order")
	}
}

func TestAgentWriteIsBlockedWithoutApprover(t *testing.T) {
	gw, url := startMock(t)
	dir := t.TempDir()

	stdout, code := runAgent(t, url, dir, "create a file named blocked.txt")
	require.Zero(t, code)

	require.FileExists(t, filepath.Join(dir, "blocked.txt"),
		"Write executes in headless mode (no approval gate)")

	lines := jsonLines(t, stdout)
	toolResults := contentsByRole(lines, "tool")
	require.Len(t, toolResults, 1)
	require.Contains(t, toolResults[0], `"success":true`, "the write must report success")

	tools := toolMessages(gw.Requests()[1].Body)
	require.Len(t, tools, 1, "the tool result must flow back to the gateway")
}

func TestAgentBashAllowlistedCommandRuns(t *testing.T) {
	_, url := startMock(t)

	stdout, code := runAgent(t, url, t.TempDir(), "run the echo command")
	require.Zero(t, code)

	toolResults := contentsByRole(jsonLines(t, stdout), "tool")
	require.Len(t, toolResults, 1)
	require.Contains(t, toolResults[0], "hello-from-bash", "allow-listed echo must actually execute")
}

func TestAgentBashOffListCommandIsBlocked(t *testing.T) {
	_, url := startMock(t)

	stdout, code := runAgent(t, url, t.TempDir(), "run the forbidden command")
	require.Zero(t, code)

	toolResults := contentsByRole(jsonLines(t, stdout), "tool")
	require.Len(t, toolResults, 1)
	require.Contains(t, toolResults[0], "Blocked:", "off-allowlist Bash must not execute headless")
}

func TestAgentHardErrorSurfacesAndExitsNonZero(t *testing.T) {
	gw, url := startMock(t)

	stdout, code := runAgent(t, url, t.TempDir(), "this always fails")
	require.NotZero(t, code, "a run that cannot reach the model must fail loudly")

	require.NotNil(t, statusOfType(jsonLines(t, stdout), "agent_error"), "an agent_error line must be emitted")
	require.Len(t, gw.Requests(), 5, "initial request plus four retries before giving up")
}

func TestChatPipedInputStreamsPlainText(t *testing.T) {
	gw, url := startMock(t)

	stdout, _, code := runCLI(t, url, t.TempDir(), "say hello\n", "chat")
	require.Zero(t, code)
	require.Contains(t, stdout, "Hello! How can I help?", "piped chat must print the streamed content")

	reqs := gw.Requests()
	require.Len(t, reqs, 1)
	require.True(t, reqs[0].Stream, "chat uses the SSE streaming path")
}
