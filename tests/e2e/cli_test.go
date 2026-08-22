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
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	tokenless "github.com/inference-gateway/tokenless"
	mockgateway "github.com/inference-gateway/tokenless/gateway"
)

var binPath string

// Model IDs used in tests — no "mock/" prefix so production code (parseProvider)
// never needs to know about mock-specific prefixes.
const (
	testModel = "openai/gpt-4o"
)

// TestMain builds the CLI once for all tests. Set INFER_E2E_BINARY to reuse
// an already-built binary (e.g. the Taskfile output) and skip the build.
func TestMain(m *testing.M) {
	var cleanup func()
	var err error
	binPath, cleanup, err = tokenless.BuildBinary(filepath.Join(repoRoot(), "cmd", "infer"), "INFER_E2E_BINARY")
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

func startMock(t *testing.T) *tokenless.Mock {
	t.Helper()
	defs, err := mockgateway.LoadFile(filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml"))
	require.NoError(t, err)
	return tokenless.StartMock(t, defs)
}

// inferEnv is the hermetic INFER_* preset: gateway URL pointed at the mock,
// gateway auto-start off, storage off, retry backoff zeroed.
func inferEnv(gatewayURL string) map[string]string {
	return map[string]string{
		"INFER_GATEWAY_URL":                      gatewayURL,
		"INFER_GATEWAY_RUN":                      "false",
		"INFER_AGENT_MODEL":                      testModel,
		"INFER_STORAGE_ENABLED":                  "false",
		"INFER_CLIENT_RETRY_INITIAL_BACKOFF_SEC": "0",
	}
}

// runCLI executes the built binary hermetically via harness.App.
func runCLI(t *testing.T, gatewayURL, dir, stdin string, args ...string) (string, string, int) {
	t.Helper()
	res := tokenless.Orchestrator{Bin: binPath, Dir: dir, Stdin: stdin, Env: inferEnv(gatewayURL)}.Run(t, args...)
	return res.Stdout, res.Stderr, res.ExitCode
}

func runAgent(t *testing.T, gatewayURL, dir, prompt string) (string, int) {
	t.Helper()
	stdout, _, code := runCLI(t, gatewayURL, dir, "", "headless", prompt)
	return stdout, code
}

func jsonLines(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	return tokenless.JSONLines(t, stdout)
}

func contentsByRole(lines []map[string]any, role string) []string {
	return tokenless.ContentsByRole(lines, role)
}

func statusOfType(lines []map[string]any, typ string) map[string]any {
	return tokenless.StatusOfType(lines, typ)
}

func writeFixtures(t *testing.T, dir string, names ...string) {
	t.Helper()
	tokenless.WriteFixtures(t, dir, names...)
}

func toolMessages(body mockgateway.CreateChatCompletionRequest) []mockgateway.Message {
	return tokenless.ToolMessages(body)
}

func TestAgentTextOnlyTerminatesAfterOneTurn(t *testing.T) {
	m := startMock(t)

	stdout, code := runAgent(t, m.URL, t.TempDir(), "say hello")
	require.Zero(t, code)

	lines := jsonLines(t, stdout)
	assistants := contentsByRole(lines, "assistant")
	require.Contains(t, assistants, "Hello! How can I help?")

	stats := statusOfType(lines, "session_stats")
	require.NotNil(t, stats, "a session_stats line must be emitted")
	require.EqualValues(t, 1, stats["requests"], "a single no-tool-call turn ends the run")
	require.EqualValues(t, 15, stats["total_tokens"], "usage from the single turn")

	reqs := m.Requests()
	require.Len(t, reqs, 1)
	require.Equal(t, "text-only", reqs[0].Scenario, "scenario must be text-only")
	require.True(t, reqs[0].Stream, "headless agent uses the streaming path")
}

// TestAgentNudgesOnIncompleteTodos guards the todo-aware completion gate
// (#946; regression via #994): a text-only reply while the model's own todo
// list still has open items is a stall, not completion — the loop injects up
// to two continuation nudges listing the open items before giving up. A run
// with no todos (TestAgentTextOnlyTerminatesAfterOneTurn) still ends on the
// first no-tool-call turn.
func TestAgentNudgesOnIncompleteTodos(t *testing.T) {
	m := startMock(t)

	stdout, code := runAgent(t, m.URL, t.TempDir(), "implement the vision feature")
	require.Zero(t, code)

	reqs := m.Requests()
	require.Len(t, reqs, 4, "TodoWrite turn, text-only strike + nudge, nudged retry + nudge, final retry then break")

	nudges := 0
	for _, m := range reqs[3].Body.Messages {
		text := m.Content.Text()
		if strings.Contains(text, "todo list still has incomplete items") {
			nudges++
			require.Contains(t, text, "[pending] wire modalities", "nudge must list the open items verbatim")
		}
	}
	require.Equal(t, 2, nudges, "exactly two continuation nudges before giving up")

	stats := statusOfType(jsonLines(t, stdout), "session_stats")
	require.NotNil(t, stats)
	require.EqualValues(t, 4, stats["requests"])
}

func TestAgentMockModeNeedsOnlyOneEnvVar(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, "headless", "-m", testModel, "say hello")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(),
		"INFER_GATEWAY_MOCK=true",
		"INFER_GATEWAY_MOCK_SCENARIOS="+filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml"),
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "stderr:\n%s", stderr.String())

	assistants := contentsByRole(jsonLines(t, stdout.String()), "assistant")
	require.Contains(t, assistants, "Hello! How can I help?")
}

func TestAgentParallelReadsExecuteAndReturnInOrder(t *testing.T) {
	m := startMock(t)
	dir := t.TempDir()
	writeFixtures(t, dir, "a.txt", "b.txt", "c.txt", "d.txt")

	stdout, code := runAgent(t, m.URL, dir, "please execute the Read tool 4 times in parallel")
	require.Zero(t, code)

	lines := jsonLines(t, stdout)
	toolResults := contentsByRole(lines, "tool")
	require.Len(t, toolResults, 4, "all four Read executions must be reported")
	for _, content := range toolResults {
		require.Contains(t, content, `"success":true`, "Read is auto-approved and must succeed")
	}
	require.Contains(t, contentsByRole(lines, "assistant"), "All four files read.")

	reqs := m.Requests()
	require.Len(t, reqs, 2, "tool turn, then answer turn")

	tools := toolMessages(reqs[1].Body)
	require.Len(t, tools, 4)
	for i, m := range tools {
		require.Equal(t, fmt.Sprintf("call_0_%d", i), *m.ToolCallID, "tool results must keep the original call order")
	}
}

func TestAgentWriteIsBlockedWithoutApprover(t *testing.T) {
	m := startMock(t)
	dir := t.TempDir()

	stdout, code := runAgent(t, m.URL, dir, "create a file named blocked.txt")
	require.Zero(t, code)

	require.NoFileExists(t, filepath.Join(dir, "blocked.txt"),
		"Write requires approval and headless runs have no approver")

	lines := jsonLines(t, stdout)
	toolResults := contentsByRole(lines, "tool")
	require.Len(t, toolResults, 1)
	require.Contains(t, toolResults[0], "Blocked:", "the rejection must carry an actionable reason")

	tools := toolMessages(m.Requests()[1].Body)
	require.Len(t, tools, 1, "the rejection must flow back to the gateway as a tool result")
}

func TestAgentBashAllowlistedCommandRuns(t *testing.T) {
	m := startMock(t)

	stdout, code := runAgent(t, m.URL, t.TempDir(), "run the echo command")
	require.Zero(t, code)

	toolResults := contentsByRole(jsonLines(t, stdout), "tool")
	require.Len(t, toolResults, 1)
	require.Contains(t, toolResults[0], "hello-from-bash", "allow-listed echo must actually execute")
}

func TestAgentBashOffListCommandIsBlocked(t *testing.T) {
	m := startMock(t)

	stdout, code := runAgent(t, m.URL, t.TempDir(), "run the forbidden command")
	require.Zero(t, code)

	toolResults := contentsByRole(jsonLines(t, stdout), "tool")
	require.Len(t, toolResults, 1)
	require.Contains(t, toolResults[0], "Blocked:", "off-allowlist Bash must not execute headless")
}

func TestAgentHardErrorSurfacesAndExitsNonZero(t *testing.T) {
	m := startMock(t)

	stdout, code := runAgent(t, m.URL, t.TempDir(), "this always fails")
	require.NotZero(t, code, "a run that cannot reach the model must fail loudly")

	require.NotNil(t, statusOfType(jsonLines(t, stdout), "agent_error"), "an agent_error line must be emitted")
	require.Len(t, m.Requests(), 5, "initial request plus four retries before giving up")
}

func TestAgentRecoversAfterTransientErrors(t *testing.T) {
	m := startMock(t)

	stdout, code := runAgent(t, m.URL, t.TempDir(), "call the flaky backend")
	require.Zero(t, code, "transient errors must be retried, not fatal")

	require.Contains(t, contentsByRole(jsonLines(t, stdout), "assistant"), "Recovered after retries.")
	require.Len(t, m.Requests(), 3,
		"two failed attempts, then the successful retry")
}

func TestChatPipedInputStreamsPlainText(t *testing.T) {
	m := startMock(t)

	stdout, _, code := runCLI(t, m.URL, t.TempDir(), "say hello\n", "chat")
	require.Zero(t, code)
	require.Contains(t, stdout, "Hello! How can I help?", "piped chat must print the streamed content")

	reqs := m.Requests()
	require.Len(t, reqs, 1)
	require.True(t, reqs[0].Stream, "chat uses the SSE streaming path")
}
