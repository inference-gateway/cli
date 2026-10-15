//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	uuid "github.com/google/uuid"

	adk "github.com/inference-gateway/adk/types"
)

const a2aServerToken = "a2a-e2e-token"

// startA2ADaemon runs `infer daemon` with only the A2A server enabled: another
// agent can send it tasks while the daemon's own agent runs with the mock
// gateway. It returns the process with its bound A2A port.
func startA2ADaemon(t *testing.T, home string, port int) *daemonProc {
	t.Helper()
	d := &daemonProc{port: port, output: &lockedBuffer{}, exited: make(chan error, 1)}
	d.cmd = exec.Command(binPath, "daemon")
	d.cmd.Dir = t.TempDir()
	d.cmd.Env = append(os.Environ(),
		"HOME="+home,
		"INFER_GATEWAY_MOCK=true",
		"INFER_GATEWAY_MOCK_SCENARIOS="+filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml"),
		"INFER_AGENT_MODEL="+testModel,
		"INFER_A2A_SERVER_ENABLED=true",
		"INFER_A2A_SERVER_PORT="+fmt.Sprint(port),
		"INFER_A2A_SERVER_TOKEN="+a2aServerToken,
	)
	d.cmd.Stdout = d.output
	d.cmd.Stderr = d.output
	require.NoError(t, d.cmd.Start())
	go func() { d.exited <- d.cmd.Wait() }()
	t.Cleanup(func() { d.stop(t) })

	deadline := time.Now().Add(20 * time.Second)
	probe := &http.Client{Timeout: time.Second}
	for {
		time.Sleep(50 * time.Millisecond)
		resp, err := probe.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err == nil {
			_ = resp.Body.Close()
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon never bound its A2A port\noutput:\n%s", d.output)
		}
	}
}

type a2aCall struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type a2aReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type polledTask struct {
	ID     string `json:"id"`
	Status struct {
		State   adk.TaskState `json:"state"`
		Message *struct {
			Parts []struct {
				Text *string `json:"text"`
			} `json:"parts"`
		} `json:"message"`
	} `json:"status"`
}

func (t *polledTask) statusText() string {
	if t.Status.Message == nil {
		return ""
	}
	texts := make([]string, 0, len(t.Status.Message.Parts))
	for _, part := range t.Status.Message.Parts {
		if part.Text != nil {
			texts = append(texts, *part.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// postA2A posts one JSON-RPC method to the server and returns the parsed envelope
// with the HTTP status. The bearer token matches the scheme the card declares.
func postA2A(t *testing.T, port int, token, method string, params any) (*a2aReply, int) {
	t.Helper()
	body, err := json.Marshal(a2aCall{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/a2a", port), bytes.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var reply a2aReply
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&reply))
	return &reply, resp.StatusCode
}

// sendPrompt submits one text task to the server and returns the created task id.
// ReturnImmediately mirrors infer's own client: the send returns the submitted
// task at once and the polling follows it to a terminal state.
func sendPrompt(t *testing.T, port int, contextID, prompt string, parts ...map[string]any) string {
	t.Helper()
	var messageParts []map[string]any
	if len(parts) > 0 {
		messageParts = parts
	} else {
		messageParts = []map[string]any{{"text": prompt}}
	}
	reply, code := postA2A(t, port, a2aServerToken, "SendMessage", map[string]any{
		"message": map[string]any{
			"messageId": uuid.NewString(),
			"role":      "ROLE_USER",
			"contextId": contextID,
			"parts":     messageParts,
		},
		"configuration": map[string]any{"returnImmediately": true},
	})
	require.Equal(t, http.StatusOK, code, "message/send accepted")
	require.Nil(t, reply.Error, "message/send error: %+v", reply.Error)

	var wrapped struct {
		Task *polledTask `json:"task"`
	}
	require.NoError(t, json.Unmarshal(reply.Result, &wrapped), "the result carries the created task")
	require.NotNil(t, wrapped.Task, "message/send returns the task")
	return wrapped.Task.ID
}

// pollTask reads the task once.
func pollTask(t *testing.T, port int, taskID string) *polledTask {
	t.Helper()
	reply, code := postA2A(t, port, a2aServerToken, "GetTask", map[string]any{"id": taskID})
	require.Equal(t, http.StatusOK, code)
	require.Nil(t, reply.Error, "tasks/get error: %+v", reply.Error)

	var task polledTask
	require.NoError(t, json.Unmarshal(reply.Result, &task))
	return &task
}

// waitTaskState polls until the task reaches one of the states. A task that lands
// on a terminal state outside the requested set fails at once: polling out a
// task that already finished wrongly just burns the deadline.
func waitTaskState(t *testing.T, port int, taskID string, states ...adk.TaskState) *polledTask {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		task := pollTask(t, port, taskID)
		for _, state := range states {
			if task.Status.State == state {
				return task
			}
		}
		if task.Status.State.IsTerminal() {
			t.Fatalf("the task finished as %s instead of %v\nmessage: %s", task.Status.State, states, task.statusText())
		}
		if time.Now().After(deadline) {
			t.Fatalf("the task never reached %v, last state %s\nmessage: %s", states, task.Status.State, task.statusText())
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// TestA2AServerEndToEnd exercises the A2A server the daemon hosts end to end:
// the card, the bearer guard, one headless run per task, and the refusals.
func TestA2AServerEndToEnd(t *testing.T) {
	port := freePort(t)
	startA2ADaemon(t, t.TempDir(), port)

	t.Run("the card is public and declares the bearer scheme", func(t *testing.T) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/.well-known/agent-card.json", port))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var card map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&card))
		require.Equal(t, "infer", card["name"])
		require.Contains(t, card, "securitySchemes")
		require.NotEmpty(t, card["securityRequirements"])
	})

	t.Run("anonymous requests are rejected", func(t *testing.T) {
		_, code := postA2A(t, port, "a-wrong-token", "ListTasks", map[string]any{})
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("a task runs headless and completes with the answer", func(t *testing.T) {
		taskID := sendPrompt(t, port, uuid.NewString(), "peer ping")
		task := waitTaskState(t, port, taskID, adk.TaskStateCompleted)
		require.Contains(t, task.statusText(), "peer-pong")
	})

	t.Run("a bang task is never executed directly", func(t *testing.T) {
		taskID := sendPrompt(t, port, uuid.NewString(), "!cat /etc/passwd")
		task := waitTaskState(t, port, taskID, adk.TaskStateCompleted)
		require.Contains(t, task.statusText(), "the bang text reached the model")
	})

	t.Run("a slash task is never run as a shortcut", func(t *testing.T) {
		taskID := sendPrompt(t, port, uuid.NewString(), "/clear")
		task := waitTaskState(t, port, taskID, adk.TaskStateCompleted)
		require.Contains(t, task.statusText(), "the slash text reached the model")
	})

	t.Run("a non-text part fails without a run", func(t *testing.T) {
		taskID := sendPrompt(t, port, uuid.NewString(), "", map[string]any{"data": map[string]any{"blob": true}})
		task := waitTaskState(t, port, taskID, adk.TaskStateFailed)
		require.Contains(t, task.statusText(), "only text parts")
	})
}
