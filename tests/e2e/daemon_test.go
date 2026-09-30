//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	uuid "github.com/google/uuid"
	websocket "github.com/gorilla/websocket"
)

const daemonToken = "e2e-token"

// lockedBuffer collects a process's output while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// daemonProc is a running `infer daemon` with only the AG-UI binding enabled.
type daemonProc struct {
	cmd      *exec.Cmd
	port     int
	output   *lockedBuffer
	exited   chan error
	stopOnce sync.Once
}

func startDaemon(t *testing.T, home string) *daemonProc {
	t.Helper()
	d := &daemonProc{port: freePort(t), output: &lockedBuffer{}, exited: make(chan error, 1)}
	d.cmd = exec.Command(binPath, "daemon")
	d.cmd.Dir = t.TempDir()
	d.cmd.Env = append(os.Environ(),
		"HOME="+home,
		"INFER_GATEWAY_MOCK=true",
		"INFER_GATEWAY_MOCK_SCENARIOS="+filepath.Join(repoRoot(), "tests", "e2e", "scenarios.yaml"),
		"INFER_AGENT_MODEL="+testModel,
		"INFER_BROWSER_USE_ENABLED=true",
		"INFER_BROWSER_USE_BACKEND=extension",
		"INFER_BROWSER_USE_EXTENSION_PORT="+strconv.Itoa(d.port),
		"INFER_BROWSER_USE_EXTENSION_TOKEN="+daemonToken,
	)
	d.cmd.Stdout = d.output
	d.cmd.Stderr = d.output
	require.NoError(t, d.cmd.Start())
	go func() { d.exited <- d.cmd.Wait() }()
	t.Cleanup(func() { d.stop(t) })

	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", d.port), time.Second)
		if err == nil {
			_ = conn.Close()
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon never bound its binding port\noutput:\n%s", d.output)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// stop sends SIGTERM and returns the daemon's exit error. It is safe to call
// more than once.
func (d *daemonProc) stop(t *testing.T) error {
	t.Helper()
	d.stopOnce.Do(func() { _ = d.cmd.Process.Signal(syscall.SIGTERM) })
	select {
	case err := <-d.exited:
		d.exited <- err
		return err
	case <-time.After(60 * time.Second):
		_ = d.cmd.Process.Kill()
		t.Fatalf("the daemon did not stop after SIGTERM\noutput:\n%s", d.output)
		return nil
	}
}

// daemonClient is one WebSocket client of the daemon binding.
type daemonClient struct {
	t      *testing.T
	d      *daemonProc
	conn   *websocket.Conn
	frames chan map[string]any
}

func dialDaemon(t *testing.T, d *daemonProc, client string) *daemonClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/ws", d.port), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	require.NoError(t, conn.WriteJSON(map[string]any{"type": "browser_hello", "token": daemonToken, "client": client, "protocol_version": 2}))
	var ack map[string]any
	require.NoError(t, conn.ReadJSON(&ack))
	require.Equal(t, "browser_hello_ack", ack["type"])
	require.Equal(t, float64(2), ack["protocol_version"])

	c := &daemonClient{t: t, d: d, conn: conn, frames: make(chan map[string]any, 256)}
	go func() {
		defer close(c.frames)
		for {
			var frame map[string]any
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}
			c.frames <- frame
		}
	}()
	return c
}

func (c *daemonClient) send(frame map[string]any) {
	c.t.Helper()
	require.NoError(c.t, c.conn.WriteJSON(frame))
}

// say sends the run_agent_input that starts a run with one user message.
func (c *daemonClient) say(content string) {
	c.t.Helper()
	c.send(map[string]any{"type": "run_agent_input", "input": map[string]any{
		"messages": []map[string]any{{"id": uuid.NewString(), "role": "user", "content": content}},
	}})
}

// resume sends the run_agent_input that answers one interrupt.
func (c *daemonClient) resume(interruptID, status string) {
	c.t.Helper()
	c.send(map[string]any{"type": "run_agent_input", "input": map[string]any{
		"resume": []map[string]any{{"interruptId": interruptID, "status": status}},
	}})
}

// next reads frames until one of the given type (or CUSTOM name) arrives.
func (c *daemonClient) next(typ string) map[string]any {
	c.t.Helper()
	timeout := time.After(60 * time.Second)
	for {
		select {
		case frame, ok := <-c.frames:
			require.True(c.t, ok, "the binding closed the connection\ndaemon output:\n%s", c.d.output)
			if frame["type"] == typ || (frame["type"] == "CUSTOM" && frame["name"] == typ) {
				return frame
			}
		case <-timeout:
			c.t.Fatalf("timed out waiting for %s\ndaemon output:\n%s", typ, c.d.output)
		}
	}
}

// run collects one run, from RUN_STARTED through its terminal event.
func (c *daemonClient) run() []map[string]any {
	c.t.Helper()
	return append([]map[string]any{c.next("RUN_STARTED")}, c.finish()...)
}

// finish collects the open run's remaining events through its terminal one.
func (c *daemonClient) finish() []map[string]any {
	c.t.Helper()
	var events []map[string]any
	timeout := time.After(60 * time.Second)
	for {
		select {
		case frame, ok := <-c.frames:
			require.True(c.t, ok, "the binding closed the connection mid-run\ndaemon output:\n%s", c.d.output)
			events = append(events, frame)
			if frame["type"] == "RUN_FINISHED" || frame["type"] == "RUN_ERROR" {
				return events
			}
		case <-timeout:
			c.t.Fatalf("timed out waiting for the run to end\nevents: %v\ndaemon output:\n%s", events, c.d.output)
		}
	}
}

// openThread starts a new thread in project and returns its conversation id,
// learned from the first run's threadId.
func openThread(c *daemonClient, project, mode string) string {
	c.t.Helper()
	c.send(map[string]any{"type": "new_session", "project_dir": project, "mode": mode, "model": testModel})
	require.Empty(c.t, c.next("MESSAGES_SNAPSHOT")["messages"])
	c.say("say hello")
	events := c.run()
	require.Equal(c.t, "RUN_FINISHED", events[len(events)-1]["type"], "events: %v", events)
	threadID, _ := events[0]["threadId"].(string)
	require.NotEmpty(c.t, threadID)
	return threadID
}

func snapshotContains(snapshot map[string]any, text string) bool {
	messages, _ := snapshot["messages"].([]any)
	for _, m := range messages {
		if msg, ok := m.(map[string]any); ok && msg["content"] == text {
			return true
		}
	}
	return false
}

func TestDaemonTwoClientsShareAThread(t *testing.T) {
	d := startDaemon(t, t.TempDir())
	project := t.TempDir()

	desktop := dialDaemon(t, d, "desktop")
	threadID := openThread(desktop, project, "auto")

	extension := dialDaemon(t, d, "extension")
	extension.send(map[string]any{"type": "resume_conversation", "project_dir": project, "id": threadID})
	require.True(t, snapshotContains(extension.next("MESSAGES_SNAPSHOT"), "Hello! How can I help?"),
		"the resume snapshot must carry the first run's reply")

	desktop.say("say hello again")
	seenByDesktop := desktop.run()
	seenByExtension := extension.run()
	require.Equal(t, threadID, seenByDesktop[0]["threadId"])
	require.Equal(t, seenByDesktop, seenByExtension, "both clients of a thread must see the same run")
}

func TestDaemonApprovalFirstAnswerWins(t *testing.T) {
	d := startDaemon(t, t.TempDir())
	project := t.TempDir()

	first := dialDaemon(t, d, "desktop")
	threadID := openThread(first, project, "standard")
	second := dialDaemon(t, d, "desktop")
	second.send(map[string]any{"type": "resume_conversation", "project_dir": project, "id": threadID})
	second.next("MESSAGES_SNAPSHOT")

	first.say("create a file named blocked.txt")
	suspended := first.run()
	require.Equal(t, suspended[len(suspended)-1], second.run()[len(suspended)-1], "both clients must see the same interrupt")
	interruptID := openInterrupt(t, suspended[len(suspended)-1])

	first.resume(interruptID, "resolved")
	second.next("RUN_STARTED")
	second.resume(interruptID, "cancelled")

	events := first.finish()
	require.Equal(t, "RUN_FINISHED", events[len(events)-1]["type"], "events: %v", events)
	require.Equal(t, map[string]any{"type": "success"}, events[len(events)-1]["outcome"])
	require.FileExists(t, filepath.Join(project, "blocked.txt"), "the first answer (approve) must win")
	select {
	case frame := <-second.frames:
		require.NotEqual(t, "RUN_ERROR", frame["type"], "a late resume is dropped, not refused: %v", frame)
	case <-time.After(time.Second):
	}
}

// openInterrupt reads the one interrupt a suspended run's RUN_FINISHED waits on.
func openInterrupt(t *testing.T, terminal map[string]any) string {
	t.Helper()
	outcome, _ := terminal["outcome"].(map[string]any)
	require.Equal(t, "interrupt", outcome["type"], "terminal: %v", terminal)
	interrupts, _ := outcome["interrupts"].([]any)
	require.Len(t, interrupts, 1)
	interrupt := interrupts[0].(map[string]any)
	require.Equal(t, "tool_call", interrupt["reason"])
	require.Equal(t, interrupt["toolCallId"], interrupt["id"])
	return interrupt["id"].(string)
}

func TestDaemonResumesAfterRestart(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	d := startDaemon(t, home)
	threadID := openThread(dialDaemon(t, d, "desktop"), project, "auto")
	require.NoError(t, d.stop(t), "SIGTERM must stop the daemon and its workers cleanly\noutput:\n%s", d.output)

	restarted := startDaemon(t, home)
	client := dialDaemon(t, restarted, "extension")
	client.send(map[string]any{"type": "resume_conversation", "project_dir": project, "id": threadID})
	require.True(t, snapshotContains(client.next("MESSAGES_SNAPSHOT"), "Hello! How can I help?"),
		"a restarted daemon must resume the stored conversation")
}

func TestDaemonWorkerCrashEndsRunWithRunError(t *testing.T) {
	d := startDaemon(t, t.TempDir())
	project := t.TempDir()
	client := dialDaemon(t, d, "desktop")
	openThread(client, project, "auto")

	client.say("crash the worker")
	events := client.run()
	terminal := events[len(events)-1]
	require.Equal(t, "RUN_ERROR", terminal["type"], "events: %v", events)
	require.Equal(t, events[0]["runId"], terminal["runId"], "the RUN_ERROR must end the run the worker left open")

	client.say("say hello")
	events = client.run()
	require.Equal(t, "RUN_FINISHED", events[len(events)-1]["type"], "a relaunched worker must serve the thread: %v", events)
}

func TestDaemonPanelRequestsAnswerFromTheProject(t *testing.T) {
	d := startDaemon(t, t.TempDir())
	project := t.TempDir()
	client := dialDaemon(t, d, "extension")

	client.send(map[string]any{"type": "list_conversations", "project_dir": project})
	require.Empty(t, client.next("conversations")["conversations"])
	client.send(map[string]any{"type": "list_models", "project_dir": project})
	require.Contains(t, client.next("models")["models"], testModel)
}
