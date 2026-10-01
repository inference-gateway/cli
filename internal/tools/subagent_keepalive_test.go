package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentrunner "github.com/inference-gateway/cli/internal/platform/agentrunner"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
)

// fakeKeepAliveChild stands in for `infer headless --keep-alive`: it prints one
// turn line for the task, then one per frame read on stdin, and returns on EOF.
func fakeKeepAliveChild(t *testing.T, turns chan<- string) func(context.Context, agentrunner.Options) (agentrunner.Result, error) {
	t.Helper()
	return func(_ context.Context, opts agentrunner.Options) (agentrunner.Result, error) {
		if opts.Stdin == nil || !opts.KeepAlive {
			t.Errorf("keep-alive child needs a stdin and --keep-alive, got %+v", opts)
			return agentrunner.Result{}, errors.New("no stdin")
		}
		turn := func(answer string) {
			line, _ := json.Marshal(scheddomain.SubagentTurnLine{Type: scheddomain.SubagentTurnLineType, SubagentResultFile: scheddomain.SubagentResultFile{
				FinalAssistant: answer, Success: true, Done: true, Stats: &scheddomain.SubagentRunStats{ToolsSucceeded: len(answer)},
			}})
			opts.OnLine(line)
			turns <- answer
		}
		turn(opts.Prompt)
		scanner := bufio.NewScanner(opts.Stdin)
		for scanner.Scan() {
			var frame runInputFrame
			if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil || frame.Type != "run_agent_input" || len(frame.Input.Messages) != 1 {
				t.Errorf("child read a frame it does not understand: %s", scanner.Text())
				continue
			}
			text, _ := frame.Input.Messages[0].Content.(string)
			turn("re:" + text)
		}
		return agentrunner.Result{}, nil
	}
}

// collectNotes returns an emit callback and a snapshot of the notes it saw.
func collectNotes() (func(scheddomain.JobSignal), func() []string) {
	var mu sync.Mutex
	var notes []string
	emit := func(sig scheddomain.JobSignal) {
		mu.Lock()
		defer mu.Unlock()
		notes = append(notes, sig.Note)
	}
	all := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), notes...)
	}
	return emit, all
}

func newKeepAliveTestJob(t *testing.T, idleTimeout time.Duration) (*headlessSubagentJob, *AgentTool) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Tools.Agent.IdleTimeout = 0
	tool := &AgentTool{config: cfg, tracker: schedinfra.NewSubagentTracker()}
	state := &scheddomain.SubagentState{ID: "k1", Label: "keeper", SessionID: "sess-k", Status: scheddomain.SubagentRunning, StartedAt: time.Now()}
	runCtx, cancel := context.WithCancel(t.Context())
	job := newKeepAliveSubagentJob(tool, AgentTaskSpec{Label: "keeper", Description: "task"}, state, runCtx, cancel)
	if !job.keepAlive || state.Input == nil || !state.Silent {
		t.Fatalf("keep-alive job not wired: keepAlive=%v input=%v silent=%v", job.keepAlive, state.Input != nil, state.Silent)
	}
	job.idleTimeout = idleTimeout
	if err := tool.tracker.AddSubagent(state); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(job.release)
	return job, tool
}

func waitTurn(t *testing.T, turns <-chan string, want string) {
	t.Helper()
	select {
	case got := <-turns:
		if got != want {
			t.Fatalf("turn = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no turn %q within 5s", want)
	}
}

// A keep-alive subagent reports each turn, runs the message the parent sends
// as the next one, idles out with one Closed note and then refuses messages.
// The emit callback reads the job back the way the UI's render does, so a
// note emitted under the job's lock would deadlock here.
func TestHeadlessSubagentJob_KeepAliveTurns(t *testing.T) {
	job, tool := newKeepAliveTestJob(t, 150*time.Millisecond)
	turns := make(chan string, 4)
	tool.runHeadless = fakeKeepAliveChild(t, turns)
	collect, notesSeen := collectNotes()
	emit := func(sig scheddomain.JobSignal) {
		_, _ = job.Idle(), job.Stats()
		collect(sig)
	}

	done := make(chan agentdomain.ToolExecutionResult, 1)
	go func() { done <- job.Run(t.Context(), emit) }()

	waitTurn(t, turns, "task")
	if s := tool.tracker.GetSubagent("k1"); s.Status != scheddomain.SubagentCompleted || !job.Idle() {
		t.Fatalf("after a done turn the subagent should be completed and idle, got %s idle=%v", s.Status, job.Idle())
	}
	if err := job.state.Input("more"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if s := tool.tracker.GetSubagent("k1"); s.Status != scheddomain.SubagentRunning || job.Idle() {
		t.Fatalf("a sent message should mark the subagent running, got %s idle=%v", s.Status, job.Idle())
	}
	waitTurn(t, turns, "re:more")
	if got := job.Stats(); got == nil || got.ToolsSucceeded != len("re:more") {
		t.Fatalf("stats should be the last turn's totals, got %+v", got)
	}

	var res agentdomain.ToolExecutionResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the idle timeout did not hang up the child")
	}
	if !res.Success {
		t.Fatalf("idle close should be a success, got %+v", res)
	}
	notes := notesSeen()
	if len(notes) != 3 || !strings.HasPrefix(notes[0], "[Subagent Completed: keeper]") || !strings.Contains(notes[1], "re:more") || !strings.HasPrefix(notes[2], "[Subagent Closed: keeper]") {
		t.Fatalf("notes = %q, want two Completed then one Closed", notes)
	}
	if !strings.Contains(notes[2], "re:more") {
		t.Fatalf("the Closed note should carry the last answer, got %q", notes[2])
	}
	if err := job.state.Input("late"); err == nil {
		t.Fatal("a closed subagent must refuse messages")
	}
}

// A turn line without done means the child already has the next turn queued:
// the subagent stays running and no idle timer is armed.
func TestHeadlessSubagentJob_KeepAliveQueuedTurnStaysRunning(t *testing.T) {
	job, tool := newKeepAliveTestJob(t, time.Second)
	emit, notesSeen := collectNotes()
	job.emit = emit

	job.completeTurn(scheddomain.SubagentResultFile{FinalAssistant: "first", Success: true, Done: false})
	if s := tool.tracker.GetSubagent("k1"); s.Status != scheddomain.SubagentRunning || job.idle != nil || job.Idle() {
		t.Fatalf("a queued next turn must keep the subagent running without an idle timer, got %s timer=%v", s.Status, job.idle != nil)
	}
	job.completeTurn(scheddomain.SubagentResultFile{FinalAssistant: "second", Success: false, Error: "boom", Done: true})
	if s := tool.tracker.GetSubagent("k1"); s.Status != scheddomain.SubagentFailed || job.idle == nil {
		t.Fatalf("a failed done turn marks the subagent failed and arms the timer, got %s timer=%v", s.Status, job.idle != nil)
	}
	notes := notesSeen()
	if len(notes) != 2 || !strings.HasPrefix(notes[0], "[Subagent Completed: keeper]") || !strings.HasPrefix(notes[1], "[Subagent Failed: keeper]") || !strings.Contains(notes[1], "Error: boom") {
		t.Fatalf("notes = %q", notes)
	}
}

// Stopping an idle subagent on request emits no note: CloseSubagent's own
// result already says so.
func TestHeadlessSubagentJob_KeepAliveCancelIsSilent(t *testing.T) {
	job, tool := newKeepAliveTestJob(t, 0)
	turns := make(chan string, 4)
	tool.runHeadless = func(ctx context.Context, opts agentrunner.Options) (agentrunner.Result, error) {
		res, err := fakeKeepAliveChild(t, turns)(ctx, opts)
		<-ctx.Done()
		return res, errors.Join(err, ctx.Err())
	}
	emit, notesSeen := collectNotes()
	done := make(chan agentdomain.ToolExecutionResult, 1)
	go func() { done <- job.Run(t.Context(), emit) }()
	waitTurn(t, turns, "task")

	job.state.CancelFunc = job.cancelRun
	job.hangUp()
	job.cancelRun()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not end the job")
	}
	if notes := notesSeen(); len(notes) != 1 {
		t.Fatalf("a requested stop adds no note, got %q", notes)
	}
	if s := tool.tracker.GetSubagent("k1"); s.Status != scheddomain.SubagentCompleted {
		t.Fatalf("status after a requested stop = %s", s.Status)
	}
}

// A child that dies before reporting a turn is announced as failed.
func TestHeadlessSubagentJob_KeepAliveCrashReportsFailure(t *testing.T) {
	job, tool := newKeepAliveTestJob(t, 0)
	tool.runHeadless = func(_ context.Context, _ agentrunner.Options) (agentrunner.Result, error) {
		return agentrunner.Result{}, errors.New("exit status 1")
	}
	emit, notesSeen := collectNotes()
	res := job.Run(t.Context(), emit)
	if res.Success {
		t.Fatalf("a crashed child is a failure, got %+v", res)
	}
	notes := notesSeen()
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "[Subagent Failed: keeper]") || !strings.Contains(notes[0], "exit status 1") {
		t.Fatalf("notes = %q", notes)
	}
	if s := tool.tracker.GetSubagent("k1"); s.Status != scheddomain.SubagentFailed {
		t.Fatalf("status = %s", s.Status)
	}
}
