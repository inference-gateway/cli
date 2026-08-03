//go:build e2e

// Package e2e runs the real `infer` binary as a subprocess against an
// in-test mock inference-gateway (internal/mockgateway), asserting on the
// headless JSON output contract, the piped-chat streaming path, tool side
// effects on disk, and the exact requests received by the gateway.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	e2etest "github.com/inference-gateway/tokenless/harness"
	mockgateway "github.com/inference-gateway/tokenless/gateway"
)

var binPath string

// TestMain builds the CLI once for all tests. Set INFER_E2E_BINARY to reuse
// an already-built binary (e.g. the Taskfile output) and skip the build.
func TestMain(m *testing.M) {
	var cleanup func()
	var err error
	binPath, cleanup, err = e2etest.BuildBinary(repoRoot(), "INFER_E2E_BINARY")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func startMock(t *testing.T) (*mockgateway.Server, string) {
	t.Helper()
	return e2etest.StartMock(t)
}

// inferEnv is the hermetic INFER_* preset: gateway URL pointed at the mock,
// gateway auto-start off, storage off, retry backoff zeroed.
func inferEnv(gatewayURL string) map[string]string {
	return map[string]string{
		"INFER_GATEWAY_URL":                      gatewayURL,
		"INFER_GATEWAY_RUN":                      "false",
		"INFER_AGENT_MODEL":                      mockgateway.DefaultModel,
		"INFER_STORAGE_ENABLED":                  "false",
		"INFER_CLIENT_RETRY_INITIAL_BACKOFF_SEC": "0",
	}
}

// runCLI executes the built binary hermetically via harness.App.
func runCLI(t *testing.T, gatewayURL, dir, stdin string, args ...string) (string, string, int) {
	t.Helper()
	res := e2etest.App{Bin: binPath, Dir: dir, Stdin: stdin, Env: inferEnv(gatewayURL)}.Run(t, args...)
	return res.Stdout, res.Stderr, res.ExitCode
}

func runAgent(t *testing.T, gatewayURL, dir, prompt string) (string, int) {
	t.Helper()
	stdout, _, code := runCLI(t, gatewayURL, dir, "", "agent", prompt)
	return stdout, code
}

func jsonLines(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	return e2etest.JSONLines(t, stdout)
}

func contentsByRole(lines []map[string]any, role string) []string {
	return e2etest.ContentsByRole(lines, role)
}

func statusOfType(lines []map[string]any, typ string) map[string]any {
	return e2etest.StatusOfType(lines, typ)
}

func writeFixtures(t *testing.T, dir string, names ...string) {
	t.Helper()
	e2etest.WriteFixtures(t, dir, names...)
}

func toolMessages(body mockgateway.CreateChatCompletionRequest) []mockgateway.Message {
	return e2etest.ToolMessages(body)
}

func TestAgentTextOnlyTerminatesAfterOneTurn(t *testing.T) {
	gw, url := startMock(t)

	stdout, code := runAgent(t, url, t.TempDir(), "say hello")
	require.Zero(t, code)

	lines := jsonLines(t, stdout)
	assistants := contentsByRole(lines, "assistant")
	require.Contains(t, assistants, "Hello! How can I help?")

	stats := statusOfType(lines, "session_stats")
	require.NotNil(t, stats, "a session_stats line must be emitted")
	require.EqualValues(t, 1, stats["requests"], "a single no-tool-call turn ends the run")
	require.EqualValues(t, 15, stats["total_tokens"], "usage from the single turn")

	reqs := gw.Requests()
	require.Len(t, reqs, 1)
	require.Equal(t, "text-only", reqs[0].Scenario, "scenario must be text-only")
	require.False(t, reqs[0].Stream, "headless agent uses the non-streaming path")
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
	require.Len(t, reqs, 2, "tool turn, then answer turn")

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

	require.NoFileExists(t, filepath.Join(dir, "blocked.txt"),
		"Write requires approval and headless runs have no approver")

	lines := jsonLines(t, stdout)
	toolResults := contentsByRole(lines, "tool")
	require.Len(t, toolResults, 1)
	require.Contains(t, toolResults[0], "Blocked:", "the rejection must carry an actionable reason")

	tools := toolMessages(gw.Requests()[1].Body)
	require.Len(t, tools, 1, "the rejection must flow back to the gateway as a tool result")
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

func TestAgentRecoversAfterTransientErrors(t *testing.T) {
	gw, url := startMock(t)

	stdout, code := runAgent(t, url, t.TempDir(), "call the flaky backend")
	require.Zero(t, code, "transient errors must be retried, not fatal")

	require.Contains(t, contentsByRole(jsonLines(t, stdout), "assistant"), "Recovered after retries.")
	require.Len(t, gw.Requests(), 3,
		"two failed attempts, then the successful retry")
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
