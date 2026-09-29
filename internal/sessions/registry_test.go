package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// launcher records every worker the registry launches. Each fake worker's
// stdout is a channel the test writes lines into, and Stop closes it.
type launcher struct {
	mu      sync.Mutex
	keys    []sessionsdomain.ThreadKey
	opts    []sessionsdomain.ThreadOptions
	workers []*sessionsmocks.FakeWorker
	lines   []chan []byte
	fail    error
}

func (l *launcher) launch(key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions) (sessionsdomain.Worker, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail != nil {
		return nil, l.fail
	}
	lines := make(chan []byte, 16)
	w := &sessionsmocks.FakeWorker{}
	w.LinesReturns(lines)
	w.StopCalls(sync.OnceFunc(func() { close(lines) }))
	l.keys = append(l.keys, key)
	l.opts = append(l.opts, opts)
	l.workers = append(l.workers, w)
	l.lines = append(l.lines, lines)
	return w, nil
}

func (l *launcher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.workers)
}

func (l *launcher) worker(i int) (*sessionsmocks.FakeWorker, chan []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.workers[i], l.lines[i]
}

func delivered(c *sessionsmocks.FakeClient) []string {
	out := make([]string, 0, c.DeliverCallCount())
	for i := range c.DeliverCallCount() {
		out = append(out, string(c.DeliverArgsForCall(i)))
	}
	return out
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// receives waits until c got exactly want, in order.
func receives(t *testing.T, c *sessionsmocks.FakeClient, want ...string) {
	t.Helper()
	eventually(t, "client frames", func() bool { return c.DeliverCallCount() >= len(want) })
	time.Sleep(20 * time.Millisecond)
	got := delivered(c)
	if len(got) != len(want) {
		t.Fatalf("client got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("client got %q, want %q", got, want)
		}
	}
}

func handle(r *Registry, c sessionsdomain.Client, frame string) {
	r.Handle(c, []byte(frame))
}

func TestRegistryTwoClientsShareAThread(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	a, b := &sessionsmocks.FakeClient{}, &sessionsmocks.FakeClient{}

	resume := `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1","model":"m1","max_turns":5}`
	handle(r, a, resume)
	handle(r, b, resume)
	if l.count() != 1 {
		t.Fatalf("launched %d workers for one thread, want 1", l.count())
	}
	if l.keys[0] != (sessionsdomain.ThreadKey{ProjectDir: "/proj", ConversationID: "conv-1"}) || l.opts[0].Model != "m1" || l.opts[0].MaxTurns != 5 {
		t.Fatalf("launched %+v with %+v", l.keys[0], l.opts[0])
	}
	worker, lines := l.worker(0)
	if worker.SendCallCount() != 2 || string(worker.SendArgsForCall(0)) != resume {
		t.Fatalf("worker got %d frames, want both resumes unchanged", worker.SendCallCount())
	}

	snapshotA := `{"type":"MESSAGES_SNAPSHOT","messages":[{"id":"1"}]}`
	snapshotB := `{"type":"MESSAGES_SNAPSHOT","messages":[{"id":"2"}]}`
	started := `{"type":"RUN_STARTED","threadId":"conv-1","runId":"run-1"}`
	content := `{"type":"TEXT_MESSAGE_CONTENT","messageId":"m","delta":"hi"}`
	for _, line := range []string{snapshotA, snapshotB, started, content} {
		lines <- []byte(line)
	}
	receives(t, a, snapshotA, started, content)
	receives(t, b, snapshotB, started, content)
}

func TestRegistryApprovalFirstAnswerWins(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	a, b := &sessionsmocks.FakeClient{}, &sessionsmocks.FakeClient{}
	handle(r, a, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
	handle(r, b, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
	worker, lines := l.worker(0)

	request := `{"type":"CUSTOM","name":"approval_request","value":{"type":"approval_request","tool_name":"Write","tool_args":"{}","tool_call_id":"call-1"}}`
	lines <- []byte(`{"type":"RUN_STARTED","threadId":"conv-1","runId":"run-1"}`)
	lines <- []byte(request)
	eventually(t, "both clients see the request", func() bool { return a.DeliverCallCount() == 2 && b.DeliverCallCount() == 2 })

	approve := `{"type":"approval_response","tool_call_id":"call-1","approved":true,"scope":"always"}`
	handle(r, a, approve)
	handle(r, b, `{"type":"approval_response","tool_call_id":"call-1","approved":false}`)

	if worker.SendCallCount() != 3 || string(worker.SendArgsForCall(2)) != approve {
		t.Fatalf("worker got %d frames, want the two resumes and only the first answer", worker.SendCallCount())
	}
	receives(t, b, `{"type":"RUN_STARTED","threadId":"conv-1","runId":"run-1"}`, request,
		`{"name":"approval_resolved","type":"CUSTOM","value":{"tool_call_id":"call-1"}}`)
	if a.DeliverCallCount() != 2 {
		t.Fatalf("the answering client got %q, want no approval_resolved", delivered(a))
	}
}

func TestRegistryWorkerCrashEndsOpenRun(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	a := &sessionsmocks.FakeClient{}
	handle(r, a, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
	_, lines := l.worker(0)

	started := `{"type":"RUN_STARTED","threadId":"conv-1","runId":"run-1"}`
	lines <- []byte(started)
	eventually(t, "RUN_STARTED", func() bool { return a.DeliverCallCount() == 1 })
	close(lines)
	receives(t, a, started, `{"message":"the session worker exited mid-run","runId":"run-1","type":"RUN_ERROR"}`)

	handle(r, a, `{"type":"interrupt"}`)
	if l.count() != 1 {
		t.Fatal("an interrupt must not relaunch a crashed worker")
	}
	handle(r, a, `{"type":"user_message","content":"again"}`)
	if l.count() != 2 || l.keys[1].ConversationID != "conv-1" {
		t.Fatalf("the next user_message must relaunch the thread's worker, launched %d", l.count())
	}
}

func TestRegistryPanelRequestsAnswerTheRequester(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	follower, requester := &sessionsmocks.FakeClient{}, &sessionsmocks.FakeClient{}
	handle(r, follower, `{"type":"new_session","project_dir":"/proj"}`)
	_, lines := l.worker(0)

	handle(r, requester, `{"type":"list_conversations","project_dir":"/proj"}`)
	handle(r, requester, `{"type":"tool_request","project_dir":"/proj","id":"req-1","tool_name":"Bash","tool_args":"{}"}`)
	if l.count() != 1 {
		t.Fatalf("panel requests must reuse the live worker in their project dir, launched %d", l.count())
	}

	conversations := `{"type":"conversations","conversations":[]}`
	approval := `{"type":"CUSTOM","name":"approval_request","value":{"tool_call_id":"req-1"}}`
	result := `{"type":"tool_result","id":"req-1","success":true,"output":"","error":""}`
	for _, line := range []string{conversations, approval, result} {
		lines <- []byte(line)
	}
	receives(t, requester, conversations, approval, result)
	if follower.DeliverCallCount() != 0 {
		t.Fatalf("the thread's follower got another client's replies: %q", delivered(follower))
	}
}

func TestRegistryPanelRequestLaunchesInProjectDir(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	a := &sessionsmocks.FakeClient{}

	handle(r, a, `{"type":"list_skills","project_dir":"/other/../proj"}`)
	if l.count() != 1 || l.keys[0].ProjectDir != "/proj" || l.keys[0].ConversationID == "" {
		t.Fatalf("expected one worker launched in /proj, got %+v", l.keys)
	}

	_, lines := l.worker(0)
	failed := `{"type":"RUN_ERROR","message":"inference gateway not available"}`
	lines <- []byte(failed)
	receives(t, a, failed)
}

func TestRegistryRejectsBadFrames(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	a := &sessionsmocks.FakeClient{}

	handle(r, a, `{"type":"new_session","project_dir":"relative/dir"}`)
	handle(r, a, `{"type":"user_message","content":"no thread yet"}`)
	handle(r, a, `{"type":"resume_conversation","project_dir":"/proj"}`)
	if l.count() != 0 {
		t.Fatalf("bad frames launched %d workers", l.count())
	}
	got := delivered(a)
	if len(got) != 3 || !strings.Contains(got[0], "absolute path") || !strings.Contains(got[1], "open a thread") || !strings.Contains(got[2], "conversation id") {
		t.Fatalf("expected three RUN_ERRORs explaining the rejection, got %q", got)
	}
}

func TestRegistryLaunchFailureReachesTheClient(t *testing.T) {
	l := &launcher{fail: errors.New("no such binary")}
	r := NewRegistry(l.launch, time.Hour)
	a := &sessionsmocks.FakeClient{}

	handle(r, a, `{"type":"new_session","project_dir":"/proj"}`)
	if got := delivered(a); len(got) != 1 || !strings.Contains(got[0], "no such binary") || !strings.Contains(got[0], "RUN_ERROR") {
		t.Fatalf("expected a RUN_ERROR naming the launch failure, got %q", got)
	}
}

func TestRegistryReapsIdleWorkers(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, 20*time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	followed, left := &sessionsmocks.FakeClient{}, &sessionsmocks.FakeClient{}
	handle(r, followed, `{"type":"new_session","project_dir":"/a"}`)
	handle(r, left, `{"type":"new_session","project_dir":"/b"}`)
	kept, _ := l.worker(0)
	reaped, lines := l.worker(1)
	lines <- []byte(`{"type":"MESSAGES_SNAPSHOT","messages":[]}`)
	eventually(t, "the snapshot reply", func() bool { return left.DeliverCallCount() == 1 })
	r.Detach(left)

	eventually(t, "the idle worker to stop", func() bool { return reaped.StopCallCount() == 1 })
	time.Sleep(60 * time.Millisecond)
	if kept.StopCallCount() != 0 {
		t.Fatal("a followed thread's worker was reaped")
	}

	cancel()
	<-done
	if kept.StopCallCount() != 1 {
		t.Fatal("shutdown must stop every worker")
	}
	handle(r, followed, `{"type":"user_message","content":"after shutdown"}`)
	if l.count() != 2 {
		t.Fatal("no worker may launch after shutdown")
	}
}

// TestRegistryRoutesWorkerBrowserCommands sends browser_command lines from two
// workers: each routes through the relay and the browser_result carrying the
// command's id goes back to the worker that asked, while the thread's clients
// never see the browser frames. A failed relay is reported as the result's
// error, never dropped, because the worker waits for the answer by id.
func TestRegistryRoutesWorkerBrowserCommands(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	var mu sync.Mutex
	var relayed []string
	noExtension := errors.New("no browser extension connected on port 52789 - install the opentask extension and set its bridge port/token to match browser_use.yaml")
	r.RouteBrowser(func(ctx context.Context, frame []byte) (json.RawMessage, error) {
		mu.Lock()
		relayed = append(relayed, string(frame))
		mu.Unlock()
		var cmd struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal(frame, &cmd); err != nil {
			return nil, err
		}
		if cmd.Action == "fail" {
			return nil, noExtension
		}
		return json.RawMessage(`{"type":"browser_result","id":"` + cmd.ID + `","title":"Example Domain"}`), nil
	})

	a, b := &sessionsmocks.FakeClient{}, &sessionsmocks.FakeClient{}
	handle(r, a, `{"type":"resume_conversation","project_dir":"/a","id":"conv-a"}`)
	handle(r, b, `{"type":"resume_conversation","project_dir":"/b","id":"conv-b"}`)
	workerA, linesA := l.worker(0)
	workerB, linesB := l.worker(1)

	linesA <- []byte(`{"type":"browser_command","id":"cmd-a","action":"navigate","url":"https://a.example","timeout_ms":123}`)
	linesB <- []byte(`{"type":"browser_command","id":"cmd-b","action":"fail","timeout_ms":123}`)

	eventually(t, "both commands relayed", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(relayed) == 2
	})
	eventually(t, "both workers answered", func() bool {
		return workerA.SendCallCount() == 2 && workerB.SendCallCount() == 2
	})
	if got := string(workerA.SendArgsForCall(1)); got != `{"type":"browser_result","id":"cmd-a","title":"Example Domain"}` {
		t.Fatalf("worker A answered with %q, want its own browser_result by id", got)
	}
	if got := string(workerB.SendArgsForCall(1)); !strings.Contains(got, `"id":"cmd-b"`) || !strings.Contains(got, "no browser extension connected") {
		t.Fatalf("worker B answered with %q, want the command's id and the no-extension wording", got)
	}
	if got := delivered(a); strings.Contains(strings.Join(got, "\n"), "browser_command") {
		t.Fatalf("worker A's clients received the browser frames: %q", got)
	}
}
