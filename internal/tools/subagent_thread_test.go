package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	aguimocks "github.com/inference-gateway/cli/tests/mocks/agui"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserinfra "github.com/inference-gateway/cli/internal/browser/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
)

const testBindingToken = "test-token"

// startDaemonBinding hosts a binding behind the counterfeiter handler, which
// plays the daemon: a resume_conversation opens the thread with a snapshot and
// each run_agent_input goes to answer. The handler's recorded calls are the
// frames the parent sent.
func startDaemonBinding(t *testing.T, answer func(conn *agui.Conn, input agui.RunAgentInput)) (agui.DialConfig, *aguimocks.FakeHandler) {
	t.Helper()
	handler := &aguimocks.FakeHandler{}
	handler.HandleCalls(func(conn *agui.Conn, frame []byte) {
		var f struct {
			Type  string             `json:"type"`
			Input agui.RunAgentInput `json:"input"`
		}
		if err := json.Unmarshal(frame, &f); err != nil {
			t.Errorf("undecodable client frame %q", frame)
			return
		}
		switch f.Type {
		case "resume_conversation":
			conn.Deliver([]byte(`{"type":"MESSAGES_SNAPSHOT","messages":[]}`))
		case "run_agent_input":
			answer(conn, f.Input)
		}
	})
	binding := agui.NewBinding(agui.BindingConfig{Port: 0, Token: testBindingToken, Handshake: browserinfra.BindingHandshake}, handler)
	if err := binding.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(binding.Close)
	return agui.DialConfig{Addr: binding.Addr(), Token: testBindingToken, Kind: subagentClientKind, Handshake: browserinfra.BindingHandshake}, handler
}

// echoTurn answers one run as the daemon's worker would: the message echoed
// back as a done turn whose tool count is the answer's length, then the text
// on turns.
func echoTurn(turns chan<- string) func(conn *agui.Conn, input agui.RunAgentInput) {
	return func(conn *agui.Conn, input agui.RunAgentInput) {
		text := "resumed"
		if len(input.Messages) == 1 {
			text, _ = input.Messages[0].Content.(string)
		}
		conn.Deliver([]byte(`{"type":"RUN_STARTED","threadId":"` + input.ThreadID + `","runId":"` + input.RunID + `"}`))
		conn.Deliver([]byte(`{"type":"TOOL_CALL_RESULT","toolCallId":"c1","content":"ok"}`))
		deliverTurn(conn, scheddomain.SubagentResultFile{FinalAssistant: text, Success: true, Done: true, Stats: &scheddomain.SubagentRunStats{ToolsSucceeded: len(text)}})
		conn.Deliver([]byte(`{"type":"RUN_FINISHED","threadId":"` + input.ThreadID + `","runId":"` + input.RunID + `","outcome":{"type":"success"},"usage":[{"model":"m","inputTokens":100,"outputTokens":20}]}`))
		turns <- text
	}
}

func deliverTurn(conn *agui.Conn, turn scheddomain.SubagentResultFile) {
	value, _ := json.Marshal(scheddomain.SubagentTurnLine{Type: scheddomain.SubagentTurnLineType, SubagentResultFile: turn})
	conn.Deliver([]byte(`{"type":"CUSTOM","name":"` + scheddomain.SubagentTurnLineType + `","value":` + string(value) + `}`))
}

// clientFrames decodes the frames of one type the handler read from the parent.
func clientFrames(handler *aguimocks.FakeHandler, frameType string) []map[string]any {
	var out []map[string]any
	for i := range handler.HandleCallCount() {
		_, frame := handler.HandleArgsForCall(i)
		var f map[string]any
		if json.Unmarshal(frame, &f) == nil && f["type"] == frameType {
			out = append(out, f)
		}
	}
	return out
}

// waitNote waits for the job to announce a turn whose note carries want.
func waitNote(t *testing.T, notes <-chan string, want string) {
	t.Helper()
	select {
	case got := <-notes:
		if !strings.Contains(got, want) {
			t.Fatalf("note = %q, want it to carry %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no note carrying %q within 5s", want)
	}
}

// threadConfig configures the binding so subagents run as daemon threads.
func threadConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Daemon.Binding = config.BindingConfig{Enabled: true, Port: 1, Token: testBindingToken}
	cfg.Tools.Agent.Mode = scheddomain.SubagentModeHeadless
	return cfg
}

func newThreadTestJob(t *testing.T, dial agui.DialConfig, idleTimeout time.Duration) (*headlessSubagentJob, *AgentTool) {
	t.Helper()
	tool := &AgentTool{config: threadConfig(), tracker: schedinfra.NewSubagentTracker()}
	tool.dialDaemon = func(context.Context) (agui.DialConfig, error) { return dial, nil }
	state := &scheddomain.SubagentState{ID: "k1", Label: "keeper", SessionID: "subagent-p-c", Status: scheddomain.SubagentRunning, StartedAt: time.Now()}
	runCtx, cancel := context.WithCancel(t.Context())
	job := newKeepAliveSubagentJob(tool, AgentTaskSpec{Label: "keeper", Description: "task"}, state, runCtx, cancel)
	if !job.keepAlive || state.Input == nil || job.stdinWrite != nil {
		t.Fatalf("thread job not wired: keepAlive=%v input=%v stdin=%v", job.keepAlive, state.Input != nil, job.stdinWrite != nil)
	}
	job.idleTimeout = idleTimeout
	if err := tool.tracker.AddSubagent(state); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(job.release)
	return job, tool
}

// A keep-alive subagent on the daemon opens its own thread, reports each turn,
// runs the parent's follow-up as the next one, idles out with one Closed note
// and then refuses messages.
func TestHeadlessSubagentJob_ThreadKeepAliveTurns(t *testing.T) {
	turns := make(chan string, 8)
	dial, handler := startDaemonBinding(t, echoTurn(turns))
	job, tool := newThreadTestJob(t, dial, 150*time.Millisecond)
	collect, notesSeen := collectNotes()
	reported := make(chan string, 8)
	emit := func(sig scheddomain.JobSignal) {
		_, _ = job.Idle(), job.Stats()
		collect(sig)
		if sig.Note != "" {
			reported <- sig.Note
		}
	}

	done := make(chan agentdomain.ToolExecutionResult, 1)
	go func() { done <- job.Run(t.Context(), emit) }()

	waitTurn(t, turns, "task")
	waitNote(t, reported, "task")
	if s := tool.tracker.GetSubagent("k1"); s.Status != scheddomain.SubagentCompleted || !job.Idle() {
		t.Fatalf("after a done turn the subagent should be completed and idle, got %s idle=%v", s.Status, job.Idle())
	}
	if err := job.state.Input("more"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if s := tool.tracker.GetSubagent("k1"); s.Status != scheddomain.SubagentRunning {
		t.Fatalf("a sent message should mark the subagent running, got %s", s.Status)
	}
	waitTurn(t, turns, "more")
	waitNote(t, reported, "more")
	if got := job.Stats(); got == nil || got.ToolsSucceeded != len("more") {
		t.Fatalf("stats should be the last turn's totals, got %+v", got)
	}

	var res agentdomain.ToolExecutionResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the idle timeout did not leave the thread")
	}
	if !res.Success {
		t.Fatalf("idle close should be a success, got %+v", res)
	}
	notes := notesSeen()
	if len(notes) != 3 || !strings.HasPrefix(notes[0], "[Subagent Completed: keeper]") || !strings.Contains(notes[1], "more") || !strings.HasPrefix(notes[2], "[Subagent Closed: keeper]") {
		t.Fatalf("notes = %q, want two Completed then one Closed", notes)
	}
	if err := job.state.Input("late"); err == nil {
		t.Fatal("a closed subagent must refuse messages")
	}

	opens := clientFrames(handler, "resume_conversation")
	if len(opens) != 1 || opens[0]["id"] != "subagent-p-c" || opens[0]["project_dir"] == "" {
		t.Fatalf("the thread must be opened once by its id in the project dir, got %v", opens)
	}
	sub, _ := opens[0]["subagent"].(map[string]any)
	if sub["depth"] != float64(1) || sub["parent"] == nil {
		t.Fatalf("the thread must say whose subagent it is and how deep, got %v", sub)
	}
}

// The parent answers an interrupted run by cancelling every interrupt, since a
// headless subagent has no approver, and the continuation's turn is reported.
func TestSubagentThread_CancelsInterrupts(t *testing.T) {
	turns := make(chan string, 8)
	echo := echoTurn(turns)
	dial, handler := startDaemonBinding(t, func(conn *agui.Conn, input agui.RunAgentInput) {
		if len(input.Resume) > 0 {
			echo(conn, input)
			return
		}
		conn.Deliver([]byte(`{"type":"RUN_STARTED","threadId":"` + input.ThreadID + `","runId":"r1"}`))
		conn.Deliver([]byte(`{"type":"RUN_FINISHED","threadId":"` + input.ThreadID + `","runId":"r1","outcome":{"type":"interrupt","interrupts":[{"id":"call_1","reason":"tool_call","toolCallId":"call_1"},{"id":"call_2","reason":"tool_call"}]}}`))
	})
	job, _ := newThreadTestJob(t, dial, 0)
	emit, _ := collectNotes()
	go job.Run(t.Context(), emit)

	waitTurn(t, turns, "resumed")
	var resumes [][]any
	for _, f := range clientFrames(handler, "run_agent_input") {
		input, _ := f["input"].(map[string]any)
		if entries, ok := input["resume"].([]any); ok && len(entries) > 0 {
			resumes = append(resumes, entries)
		}
	}
	if len(resumes) != 1 {
		t.Fatalf("want one resume, got %v", resumes)
	}
	entries := resumes[0]
	if len(entries) != 2 || entries[0].(map[string]any)["status"] != "cancelled" || entries[1].(map[string]any)["interruptId"] != "call_2" {
		t.Fatalf("every interrupt must be cancelled, got %v", entries)
	}
}

// A daemon that drops the thread mid-turn is announced as a failure.
func TestHeadlessSubagentJob_ThreadGoneMidTurnReportsFailure(t *testing.T) {
	started := make(chan *agui.Conn, 1)
	dial, _ := startDaemonBinding(t, func(conn *agui.Conn, input agui.RunAgentInput) {
		conn.Deliver([]byte(`{"type":"RUN_STARTED","threadId":"` + input.ThreadID + `","runId":"r1"}`))
		started <- conn
	})
	job, tool := newThreadTestJob(t, dial, 0)
	emit, notesSeen := collectNotes()
	done := make(chan agentdomain.ToolExecutionResult, 1)
	go func() { done <- job.Run(t.Context(), emit) }()

	select {
	case conn := <-started:
		conn.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never started")
	}
	var res agentdomain.ToolExecutionResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the lost connection did not end the job")
	}
	if res.Success {
		t.Fatalf("a lost thread is a failure, got %+v", res)
	}
	if notes := notesSeen(); len(notes) != 1 || !strings.HasPrefix(notes[0], "[Subagent Failed: keeper]") {
		t.Fatalf("notes = %q", notes)
	}
	if s := tool.tracker.GetSubagent("k1"); s.Status != scheddomain.SubagentFailed {
		t.Fatalf("status = %s", s.Status)
	}
}

// A blocking fan-out runs each subagent as its own thread and returns once
// every first turn is done.
func TestAgentTool_ThreadBlockingFanOut(t *testing.T) {
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	turns := make(chan string, 8)
	dial, handler := startDaemonBinding(t, echoTurn(turns))
	cfg := threadConfig()
	cfg.Tools.Agent.Wait = true
	tool, _ := newSupervisedAgentTool(t, cfg)
	tool.dialDaemon = func(context.Context) (agui.DialConfig, error) { return dial, nil }

	res, err := tool.Execute(t.Context(), map[string]any{"tasks": []any{
		map[string]any{"description": "task A", "label": "A"},
		map[string]any{"description": "task B", "label": "B"},
	}})
	if err != nil || res == nil || !res.Success {
		t.Fatalf("Execute: res=%+v err=%v", res, err)
	}
	data := res.Data.(AgentToolResult)
	if len(data.Subagents) != 2 {
		t.Fatalf("subagents = %+v", data.Subagents)
	}
	for _, s := range data.Subagents {
		if !s.Success || s.Result != "task "+s.Label || s.Stats == nil || !strings.HasPrefix(s.SessionID, "subagent-") {
			t.Fatalf("subagent result not harvested: %+v", s)
		}
	}
	if opens := clientFrames(handler, "resume_conversation"); len(opens) != 2 || opens[0]["id"] == opens[1]["id"] {
		t.Fatalf("each subagent opens its own thread, got %v", opens)
	}
}

// An async subagent's thread names the session that spawned it, so the
// daemon's worker reports its turns back to that parent.
func TestAgentTool_ThreadAsyncCarriesParentSession(t *testing.T) {
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	turns := make(chan string, 8)
	dial, handler := startDaemonBinding(t, echoTurn(turns))
	cfg := threadConfig()
	cfg.Tools.Agent.Wait = false
	tool, _ := newSupervisedAgentTool(t, cfg)
	tool.dialDaemon = func(context.Context) (agui.DialConfig, error) { return dial, nil }

	ctx := agentdomain.WithSessionID(t.Context(), "parent-session")
	res, err := tool.Execute(ctx, map[string]any{"tasks": []any{map[string]any{"description": "task A", "label": "A"}}})
	if err != nil || res == nil || !res.Success {
		t.Fatalf("Execute: res=%+v err=%v", res, err)
	}
	waitTurn(t, turns, "task A")
	opens := clientFrames(handler, "resume_conversation")
	if len(opens) != 1 {
		t.Fatalf("the subagent opens one thread, got %v", opens)
	}
	sub, _ := opens[0]["subagent"].(map[string]any)
	if sub["parent"] != "parent-session" {
		t.Fatalf("the thread must name the parent session, got %v", sub)
	}
}
