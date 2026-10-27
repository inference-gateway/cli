package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	uuid "github.com/google/uuid"

	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// threadOpenTimeout bounds the wait for the worker's history snapshot: a
// worker launch starts the project's gateway and MCP servers first.
const threadOpenTimeout = 90 * time.Second

// subagentClientKind is the client kind the parent declares in its hello.
const subagentClientKind = "subagent-parent"

var errThreadClosed = errors.New("the daemon closed the subagent's thread")

// subagentThread follows one subagent's thread on the daemon as a client of
// its binding, the way the desktop does, so the daemon owns the worker and
// every other client of the thread sees the same turns. It runs each message
// as a turn and hands the turns the worker reports to onTurn.
type subagentThread struct {
	sessionID string
	onTurn    func(scheddomain.SubagentResultFile)
	onFrame   func([]byte)

	opened chan error
	closed chan struct{}

	mu      sync.Mutex
	conn    *agui.Conn
	running bool
	stopped bool
	err     error
}

// openSubagentThread dials the binding, makes the connection follow the
// subagent's thread in projectDir with opts and returns once the worker
// answered with its history. A nil onFrame ignores the streamed frames.
func openSubagentThread(ctx context.Context, dial agui.DialConfig, projectDir, sessionID string, opts sessionsdomain.ThreadOptions, onTurn func(scheddomain.SubagentResultFile), onFrame func([]byte)) (*subagentThread, error) {
	t := &subagentThread{sessionID: sessionID, onTurn: onTurn, onFrame: onFrame, opened: make(chan error, 1), closed: make(chan struct{})}
	conn, err := agui.Dial(ctx, dial, t)
	if err != nil {
		return nil, err
	}
	if err := conn.Send(resumeFrame(projectDir, sessionID, opts)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("opening the subagent's thread: %w", err)
	}
	if err := t.awaitOpen(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return t, nil
}

func (t *subagentThread) awaitOpen(ctx context.Context) error {
	select {
	case err := <-t.opened:
		return err
	case <-time.After(threadOpenTimeout):
		return errors.New("the daemon did not open the subagent's thread in time")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// run sends text as the thread's next turn.
func (t *subagentThread) run(text string) error {
	frame := runInputFrame{Type: ipc.RunAgentInputFrameType, Input: agui.RunAgentInput{
		ThreadID: t.sessionID, RunID: uuid.NewString(),
		Messages: []agui.Message{{ID: uuid.NewString(), Role: "user", Content: text}},
	}}
	return t.send(frame)
}

func (t *subagentThread) send(frame any) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return errThreadClosed
	}
	return conn.Send(data)
}

// stop interrupts a running turn and leaves the thread, which the daemon
// reaps once nobody follows it. Safe to call more than once.
func (t *subagentThread) stop() {
	t.mu.Lock()
	conn, running := t.conn, t.running
	t.stopped = true
	t.mu.Unlock()
	if conn == nil {
		return
	}
	if running {
		_ = conn.Send([]byte(`{"type":"interrupt"}`))
	}
	conn.Close()
}

// alive reports whether the connection to the daemon still stands.
func (t *subagentThread) alive() bool {
	select {
	case <-t.closed:
		return false
	default:
		return true
	}
}

// done is closed once the connection to the daemon is gone.
func (t *subagentThread) done() <-chan struct{} {
	return t.closed
}

// closeErr is why the connection ended, nil for a stop the parent asked for.
func (t *subagentThread) closeErr() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// Attach keeps the connection the dial produced.
func (t *subagentThread) Attach(conn *agui.Conn) {
	t.mu.Lock()
	t.conn = conn
	t.mu.Unlock()
}

// threadFrame is what the parent reads off a frame of the thread.
type threadFrame struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Message string          `json:"message"`
	Value   json.RawMessage `json:"value"`
	Outcome struct {
		Type       string           `json:"type"`
		Interrupts []agui.Interrupt `json:"interrupts"`
	} `json:"outcome"`
}

// Handle reads one frame of the thread: the snapshot that opens it, the run
// boundaries, the turns the worker reports and the failures. An interrupted
// run resumes with every interrupt cancelled, as a headless subagent has no
// approver. ponytail: leave it open once a desktop follower may approve.
func (t *subagentThread) Handle(_ *agui.Conn, frame []byte) {
	if t.onFrame != nil {
		t.onFrame(frame)
	}
	var f threadFrame
	if json.Unmarshal(frame, &f) != nil {
		return
	}
	switch f.Type {
	case "MESSAGES_SNAPSHOT":
		t.open(nil)
	case "RUN_STARTED":
		t.setRunning(true)
	case "RUN_FINISHED":
		t.setRunning(false)
		if f.Outcome.Type == "interrupt" {
			t.cancelInterrupts(f.Outcome.Interrupts)
		}
	case "RUN_ERROR":
		t.setRunning(false)
		if !t.open(errors.New(f.Message)) {
			t.onTurn(scheddomain.SubagentResultFile{SessionID: t.sessionID, Error: f.Message, Done: true})
		}
	case "CUSTOM":
		if f.Name == scheddomain.SubagentTurnLineType {
			t.turn(f.Value)
		}
	}
}

func (t *subagentThread) turn(value json.RawMessage) {
	var turn scheddomain.SubagentResultFile
	if err := json.Unmarshal(value, &turn); err != nil {
		logger.Warn("subagent turn frame undecodable", "session_id", t.sessionID, "error", err)
		return
	}
	t.onTurn(turn)
}

func (t *subagentThread) cancelInterrupts(interrupts []agui.Interrupt) {
	resume := make([]agui.ResumeEntry, 0, len(interrupts))
	for _, in := range interrupts {
		resume = append(resume, agui.ResumeEntry{InterruptID: in.ID, Status: agui.ResumeStatusCancelled})
	}
	frame := runInputFrame{Type: ipc.RunAgentInputFrameType, Input: agui.RunAgentInput{ThreadID: t.sessionID, RunID: uuid.NewString(), Resume: resume}}
	if err := t.send(frame); err != nil {
		logger.Warn("subagent interrupt could not be cancelled", "session_id", t.sessionID, "error", err)
	}
}

// open delivers the open outcome once and reports whether this call was it.
func (t *subagentThread) open(err error) bool {
	select {
	case t.opened <- err:
		return true
	default:
		return false
	}
}

func (t *subagentThread) setRunning(running bool) {
	t.mu.Lock()
	t.running = running
	t.mu.Unlock()
}

// Detach records a connection that ended. One that ends mid-turn without a
// stop is the daemon or its worker going away, which the job reports as a
// failure.
func (t *subagentThread) Detach(conn *agui.Conn) {
	conn.Close()
	t.mu.Lock()
	if t.conn == conn {
		t.conn = nil
	}
	if t.running && !t.stopped {
		t.err = errThreadClosed
	}
	t.mu.Unlock()
	t.open(errThreadClosed)
	close(t.closed)
}

// resumeFrame is the resume_conversation frame that opens (or creates) the
// subagent's thread with its options.
func resumeFrame(projectDir, sessionID string, opts sessionsdomain.ThreadOptions) []byte {
	frame := struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		ProjectDir string `json:"project_dir"`
		sessionsdomain.ThreadOptions
	}{Type: "resume_conversation", ID: sessionID, ProjectDir: projectDir, ThreadOptions: opts}
	data, _ := json.Marshal(frame)
	return data
}
