package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

var hooksKey = sessionsdomain.ThreadKey{ProjectDir: "/proj", ConversationID: "conv-1"}

func TestQuiesceInterruptsTheOpenRunAndStopsTheWorker(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	c := &sessionsmocks.FakeClient{}
	handle(r, c, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
	worker, lines := l.worker(0)
	lines <- []byte(`{"type":"MESSAGES_SNAPSHOT","messages":[]}`)
	lines <- []byte(`{"type":"RUN_STARTED","threadId":"conv-1","runId":"run-1"}`)
	receives(t, c, `{"type":"MESSAGES_SNAPSHOT","messages":[]}`, `{"type":"RUN_STARTED","threadId":"conv-1","runId":"run-1"}`)

	worker.SendCalls(func(frame []byte) error {
		if strings.Contains(string(frame), `"interrupt"`) {
			lines <- []byte(`{"type":"RUN_FINISHED","threadId":"conv-1","runId":"run-1","outcome":{"type":"interrupt","interrupts":[{"id":"int-1","reason":"tool_call","toolCallId":"call-1"}]}}`)
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	interrupts, err := r.Quiesce(ctx, hooksKey)
	if err != nil {
		t.Fatalf("Quiesce: %v", err)
	}
	var parsed []map[string]any
	if json.Unmarshal(interrupts, &parsed) != nil || len(parsed) != 1 || parsed[0]["toolCallId"] != "call-1" {
		t.Fatalf("interrupts = %s", interrupts)
	}
	if worker.StopCallCount() != 1 {
		t.Fatalf("worker Stop called %d times, want 1", worker.StopCallCount())
	}
	r.mu.Lock()
	thread := r.threads[hooksKey]
	r.mu.Unlock()
	if thread == nil || thread.worker != nil || len(thread.clients) != 1 {
		t.Fatalf("after Quiesce the thread must stay with its client and no worker: %+v", thread)
	}

	handle(r, c, `{"type":"run_agent_input","input":{"threadId":"conv-1","messages":[{"id":"m","role":"user","content":"again"}]}}`)
	if l.count() != 2 {
		t.Fatalf("a frame after Quiesce launched %d workers in total, want a relaunch", l.count())
	}
}

func TestQuiesceWithoutARunStopsTheWorkerAtRest(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	handle(r, &sessionsmocks.FakeClient{}, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
	worker, _ := l.worker(0)

	interrupts, err := r.Quiesce(t.Context(), hooksKey)
	if err != nil || interrupts != nil {
		t.Fatalf("Quiesce = %s, %v", interrupts, err)
	}
	if worker.StopCallCount() != 1 || worker.SendCallCount() != 1 {
		t.Fatalf("an idle worker is stopped without an interrupt: stops %d, sends %d", worker.StopCallCount(), worker.SendCallCount())
	}
	if _, err := r.Quiesce(t.Context(), sessionsdomain.ThreadKey{ProjectDir: "/none", ConversationID: "x"}); err != nil {
		t.Fatalf("Quiesce of an unknown thread: %v", err)
	}
}

func TestQuiesceGivesUpWhenTheRunNeverEnds(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	c := &sessionsmocks.FakeClient{}
	handle(r, c, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
	_, lines := l.worker(0)
	lines <- []byte(`{"type":"RUN_STARTED","threadId":"conv-1","runId":"run-1"}`)
	eventually(t, "the run to open", func() bool { return c.DeliverCallCount() == 1 })

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := r.Quiesce(ctx, hooksKey); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Quiesce = %v, want the deadline", err)
	}
}

func TestDeliverReachesTheThreadsFollowers(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	a, b, other := &sessionsmocks.FakeClient{}, &sessionsmocks.FakeClient{}, &sessionsmocks.FakeClient{}
	handle(r, a, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
	handle(r, b, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
	handle(r, other, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-2"}`)

	r.Deliver(hooksKey, []byte(`{"type":"session_moved"}`))
	receives(t, a, `{"type":"session_moved"}`)
	receives(t, b, `{"type":"session_moved"}`)
	if other.DeliverCallCount() != 0 {
		t.Fatal("a client of another thread got the frame")
	}
	r.Deliver(sessionsdomain.ThreadKey{ProjectDir: "/none", ConversationID: "x"}, []byte(`{}`))
}

func TestRequestReturnsThePanelReply(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	done := make(chan struct{})
	var reply []byte
	var err error
	go func() {
		defer close(done)
		reply, err = r.Request(t.Context(), []byte(`{"type":"export_conversation","project_dir":"/proj","id":"conv-1"}`), "conversation_export")
	}()
	eventually(t, "a worker to launch", func() bool { return l.count() == 1 })
	worker, lines := l.worker(0)
	eventually(t, "the request to reach the worker", func() bool { return worker.SendCallCount() == 1 })
	lines <- []byte(`{"type":"thread_activity","threads":[]}`)
	lines <- []byte(`{"type":"conversation_export","id":"conv-1","digest":"abc"}`)
	<-done
	if err != nil || !strings.Contains(string(reply), `"digest":"abc"`) {
		t.Fatalf("Request = %s, %v", reply, err)
	}
	r.mu.Lock()
	subscribed := len(r.subs) + len(r.all)
	r.mu.Unlock()
	if subscribed != 0 {
		t.Fatalf("the request client is still attached: %d", subscribed)
	}
}

func TestRequestFailsOnARunError(t *testing.T) {
	l := &launcher{fail: errors.New("no binary")}
	r := NewRegistry(l.launch, time.Hour)
	_, err := r.Request(t.Context(), []byte(`{"type":"export_conversation","project_dir":"/proj","id":"conv-1"}`), "conversation_export")
	if err == nil || !strings.Contains(err.Error(), "no binary") {
		t.Fatalf("Request = %v, want the launch failure", err)
	}
	_, err = r.Request(t.Context(), []byte(`{"type":"export_conversation","project_dir":"relative","id":"conv-1"}`), "conversation_export")
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("Request = %v, want the project_dir refusal", err)
	}
}

func TestIdleTracksThreadsAndClients(t *testing.T) {
	l := &launcher{}
	r := NewRegistry(l.launch, time.Hour)
	if _, idle := r.Idle(); !idle {
		t.Fatal("a fresh registry is idle")
	}
	c := &sessionsmocks.FakeClient{}
	r.Attach(c)
	if _, idle := r.Idle(); idle {
		t.Fatal("an attached client is not idle")
	}
	handle(r, c, `{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`)
	_, lines := l.worker(0)
	lines <- []byte(`{"type":"MESSAGES_SNAPSHOT","messages":[]}`)
	receives(t, c, `{"threads":[],"type":"thread_activity"}`, `{"type":"MESSAGES_SNAPSHOT","messages":[]}`)
	r.Detach(c)
	if _, idle := r.Idle(); idle {
		t.Fatal("a thread with a worker is not idle")
	}
	r.reap(time.Now().Add(2 * time.Hour))
	since, idle := r.Idle()
	if !idle || time.Since(since) > time.Second {
		t.Fatalf("after reaping the registry is idle since just now: %v %v", since, idle)
	}
}
