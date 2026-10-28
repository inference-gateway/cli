package transfer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

var testKey = sessionsdomain.ThreadKey{ProjectDir: "/proj", ConversationID: "conv-1"}

// launcher records the process workers the seam asked for.
type launcher struct {
	mu    sync.Mutex
	keys  []sessionsdomain.ThreadKey
	opts  []sessionsdomain.ThreadOptions
	lines []chan []byte
}

func (l *launcher) launch(key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions) (sessionsdomain.Worker, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	lines := make(chan []byte, 16)
	w := &sessionsmocks.FakeWorker{}
	w.LinesReturns(lines)
	w.StopCalls(sync.OnceFunc(func() { close(lines) }))
	l.keys = append(l.keys, key)
	l.opts = append(l.opts, opts)
	l.lines = append(l.lines, lines)
	return w, nil
}

func (l *launcher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.keys)
}

func fakeEnvironment(name string, kind agentdomain.EnvironmentKind) *agentdomainmocks.FakeEnvironment {
	e := &agentdomainmocks.FakeEnvironment{}
	e.NameReturns(name)
	e.KindReturns(kind)
	return e
}

func newService(t *testing.T, l *launcher, environments ...agentdomain.Environment) (*Service, *storage.MemoryStorage) {
	t.Helper()
	store := storage.NewMemoryStorage()
	entries := []config.EnvironmentEntry{}
	for _, e := range environments {
		if e.Kind() != agentdomain.EnvironmentKindLocal {
			entries = append(entries, config.EnvironmentEntry{Name: e.Name(), Kind: config.EnvironmentKind(e.Kind()), Address: "peer:1", TLS: config.PeerTLSConfig{CAFile: "ca"}})
		}
	}
	s := New(Config{Identity: "laptop", WorkspaceRoot: t.TempDir(), Entries: entries}, Deps{Leases: store, Environments: environments, Launch: l.launch})
	return s, store
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func firstLine(t *testing.T, w sessionsdomain.Worker) string {
	t.Helper()
	select {
	case line, ok := <-w.Lines():
		if !ok {
			t.Fatal("the worker's lines closed without a line")
		}
		return string(line)
	case <-time.After(3 * time.Second):
		t.Fatal("no worker line in time")
		return ""
	}
}

func TestLazyWorkerReplaysAndRelays(t *testing.T) {
	w := newLazyWorker()
	if err := w.Send([]byte(`{"type":"resume_conversation"}`)); err != nil {
		t.Fatal(err)
	}
	inner := &sessionsmocks.FakeWorker{}
	lines := make(chan []byte, 1)
	inner.LinesReturns(lines)
	w.attach(inner)
	if inner.SendCallCount() != 1 || string(inner.SendArgsForCall(0)) != `{"type":"resume_conversation"}` {
		t.Fatalf("the buffered frame was not replayed: %d sends", inner.SendCallCount())
	}
	lines <- []byte(`{"type":"MESSAGES_SNAPSHOT"}`)
	if got := firstLine(t, w); got != `{"type":"MESSAGES_SNAPSHOT"}` {
		t.Fatalf("relayed %q", got)
	}
	_ = w.Send([]byte(`{"type":"interrupt"}`))
	if inner.SendCallCount() != 2 {
		t.Fatal("a frame after attach did not reach the inner worker")
	}
	close(lines)
	eventually(t, "lines to close", func() bool {
		select {
		case _, ok := <-w.Lines():
			return !ok
		default:
			return false
		}
	})
	w.Stop()
	if inner.StopCallCount() != 1 {
		t.Fatal("Stop did not reach the inner worker")
	}
}

func TestLazyWorkerFailsWithOneRunError(t *testing.T) {
	w := newLazyWorker()
	w.fail("nowhere to run")
	if got := firstLine(t, w); !strings.Contains(got, "RUN_ERROR") || !strings.Contains(got, "nowhere to run") {
		t.Fatalf("got %q", got)
	}
	if _, ok := <-w.Lines(); ok {
		t.Fatal("lines stay open after the failure")
	}
	if err := w.Send([]byte(`{}`)); err == nil {
		t.Fatal("a send after the failure succeeded")
	}
	stopped := newLazyWorker()
	stopped.Stop()
	late := &sessionsmocks.FakeWorker{}
	stopped.attach(late)
	if late.StopCallCount() != 1 {
		t.Fatal("a worker attached after Stop was not stopped")
	}
}

func TestLaunchClaimsTheSessionAndRunsLocally(t *testing.T) {
	l := &launcher{}
	s, store := newService(t, l)
	s.remember("conv-1", map[string]string{"GITHUB_TOKEN": "ghp"})

	w, err := s.LaunchWorker(testKey, sessionsdomain.ThreadOptions{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the process worker to launch", func() bool { return l.count() == 1 })
	if l.keys[0] != testKey || l.opts[0].Model != "m" || len(l.opts[0].Env) != 1 || l.opts[0].Env[0] != "GITHUB_TOKEN=ghp" {
		t.Fatalf("launched %+v with %+v", l.keys[0], l.opts[0])
	}
	lease, found, _ := store.GetLease(context.Background(), "conv-1")
	if !found || lease.Environment != "laptop" || lease.Workspace != "/proj" {
		t.Fatalf("lease = %+v, %v", lease, found)
	}
	l.lines[0] <- []byte(`{"type":"RUN_STARTED"}`)
	if got := firstLine(t, w); got != `{"type":"RUN_STARTED"}` {
		t.Fatalf("relayed %q", got)
	}
	w.Stop()
}

func TestLaunchRefusesAForeignLiveLeaseItCannotReach(t *testing.T) {
	l := &launcher{}
	k8s := fakeEnvironment("k8s", agentdomain.EnvironmentKindKubernetes)
	k8s.ReachReturns(agentdomain.Endpoint{}, errors.New("cluster down"))
	s, store := newService(t, l, k8s)
	_, _, _ = store.ClaimLease(context.Background(), agentdomain.Lease{ConversationID: "conv-1", Environment: "k8s", Workspace: "/w", Until: time.Now().Add(time.Hour)})

	w, _ := s.LaunchWorker(testKey, sessionsdomain.ThreadOptions{})
	got := firstLine(t, w)
	if !strings.Contains(got, "RUN_ERROR") || !strings.Contains(got, "owned by environment k8s") || !strings.Contains(got, "cluster down") {
		t.Fatalf("got %q", got)
	}
	if l.count() != 0 {
		t.Fatal("a process worker was launched for a session another environment owns")
	}
	lease, _, _ := store.GetLease(context.Background(), "conv-1")
	if lease.Environment != "k8s" {
		t.Fatalf("the lease changed hands: %+v", lease)
	}
}

func TestLaunchReclaimsALapsedLeaseOfAnUnreachableEnvironment(t *testing.T) {
	l := &launcher{}
	k8s := fakeEnvironment("k8s", agentdomain.EnvironmentKindKubernetes)
	k8s.ReachReturns(agentdomain.Endpoint{}, errors.New("cluster down"))
	s, store := newService(t, l, k8s)
	_, _, _ = store.ClaimLease(context.Background(), agentdomain.Lease{ConversationID: "conv-1", Environment: "k8s", Until: time.Now().Add(-time.Minute)})

	w, _ := s.LaunchWorker(testKey, sessionsdomain.ThreadOptions{})
	eventually(t, "the session to be reclaimed", func() bool { return l.count() == 1 })
	lease, _, _ := store.GetLease(context.Background(), "conv-1")
	if lease.Environment != "laptop" {
		t.Fatalf("lease = %+v", lease)
	}
	w.Stop()
}

func TestLaunchRefusesAnUnknownOwner(t *testing.T) {
	l := &launcher{}
	s, store := newService(t, l)
	_, _, _ = store.ClaimLease(context.Background(), agentdomain.Lease{ConversationID: "conv-1", Environment: "ghost", Until: time.Now().Add(time.Hour)})
	w, _ := s.LaunchWorker(testKey, sessionsdomain.ThreadOptions{})
	if got := firstLine(t, w); !strings.Contains(got, "not registered") {
		t.Fatalf("got %q", got)
	}
}
