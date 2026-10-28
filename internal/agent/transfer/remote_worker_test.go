package transfer

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	aguimocks "github.com/inference-gateway/cli/tests/mocks/agui"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

var testHandshake = agui.Handshake{Hello: "hello", Ack: "hello_ack"}

// peerBinding is an in-test daemon: it records the frames it receives and
// lets the test answer on the connection.
type peerBinding struct {
	binding *agui.Binding
	mu      sync.Mutex
	conns   []*agui.Conn
	frames  [][]byte
}

func startPeer(t *testing.T, environment string, handle func(conn *agui.Conn, frame []byte)) *peerBinding {
	t.Helper()
	p := &peerBinding{}
	handler := &aguimocks.FakeHandler{}
	handler.AttachCalls(func(conn *agui.Conn) {
		p.mu.Lock()
		p.conns = append(p.conns, conn)
		p.mu.Unlock()
	})
	handler.HandleCalls(func(conn *agui.Conn, frame []byte) {
		p.mu.Lock()
		p.frames = append(p.frames, append([]byte(nil), frame...))
		p.mu.Unlock()
		if handle != nil {
			handle(conn, frame)
		}
	})
	p.binding = agui.NewBinding(agui.BindingConfig{
		Token: "t", Handshake: testHandshake,
		AckAttrs: map[string]any{agentinfra.AttrEnvironment: environment, agentinfra.AttrVersion: "test", agentinfra.AttrStorageID: "store-b"},
	}, handler)
	if err := p.binding.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.binding.Close)
	return p
}

func (p *peerBinding) received(t *testing.T, n int) [][]byte {
	t.Helper()
	eventually(t, "peer frames", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.frames) >= n
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]byte(nil), p.frames...)
}

func (p *peerBinding) conn(t *testing.T) *agui.Conn {
	t.Helper()
	eventually(t, "a peer connection", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return len(p.conns) > 0
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conns[0]
}

func (p *peerBinding) endpoint() agentdomain.Endpoint {
	return agentdomain.Endpoint{Addr: p.binding.Addr(), Token: "t"}
}

func TestRemoteWorkerRewritesTheProjectDirAndDropsDaemonFrames(t *testing.T) {
	peer := startPeer(t, "peer", nil)
	environment := fakeEnvironment("peer", agentdomain.EnvironmentKindDaemon)
	environment.ReachReturns(peer.endpoint(), nil)
	s, _ := newService(t, &launcher{}, environment)
	s.dial.Handshake = testHandshake

	lease := agentdomain.Lease{ConversationID: "conv-1", Environment: "peer", Workspace: "/remote/ws", Until: time.Now().Add(time.Hour)}
	w, err := s.connectRemote(t.Context(), testKey, lease)
	if err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	if err := w.Send([]byte(`{"type": "resume_conversation", "project_dir": "/proj", "id": "conv-1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Send([]byte(`{"type":"run_agent_input","input":{"threadId":"conv-1"}}`)); err != nil {
		t.Fatal(err)
	}
	frames := peer.received(t, 2)
	var resume map[string]any
	_ = json.Unmarshal(frames[0], &resume)
	if resume["project_dir"] != "/remote/ws" || resume["id"] != "conv-1" {
		t.Fatalf("peer got %s", frames[0])
	}
	if strings.Contains(string(frames[1]), "project_dir") {
		t.Fatalf("a frame without project_dir gained one: %s", frames[1])
	}

	conn := peer.conn(t)
	conn.Deliver([]byte(`{"type":"thread_activity","threads":[]}`))
	conn.Deliver([]byte(`{"type":"MESSAGES_SNAPSHOT","messages":[]}`))
	if got := firstLine(t, w); got != `{"type":"MESSAGES_SNAPSHOT","messages":[]}` {
		t.Fatalf("relayed %q, daemon frames must be dropped", got)
	}
	w.Stop()
	eventually(t, "lines to close", func() bool {
		select {
		case _, ok := <-w.Lines():
			return !ok
		default:
			return false
		}
	})
	if environment.ReachCallCount() != 1 || environment.EnsureCallCount() != 0 {
		t.Fatalf("a relay reached the environment %d times and ensured it %d times", environment.ReachCallCount(), environment.EnsureCallCount())
	}
}

func TestRemoteWorkerRefusesAPeerWithAnotherIdentity(t *testing.T) {
	peer := startPeer(t, "someone-else", nil)
	environment := fakeEnvironment("peer", agentdomain.EnvironmentKindDaemon)
	environment.ReachReturns(peer.endpoint(), nil)
	s, _ := newService(t, &launcher{}, environment)
	s.dial.Handshake = testHandshake

	_, err := s.connectRemote(t.Context(), testKey, agentdomain.Lease{Environment: "peer"})
	if err == nil || !strings.Contains(err.Error(), "identifies as") {
		t.Fatalf("connectRemote = %v", err)
	}
}

func TestLaunchRelaysToTheOwningEnvironment(t *testing.T) {
	peer := startPeer(t, "peer", nil)
	environment := fakeEnvironment("peer", agentdomain.EnvironmentKindDaemon)
	environment.ReachReturns(peer.endpoint(), nil)
	l := &launcher{}
	s, store := newService(t, l, environment)
	s.dial.Handshake = testHandshake
	_, _, _ = store.ClaimLease(context.Background(), agentdomain.Lease{ConversationID: "conv-1", Environment: "peer", Workspace: "/remote", Until: time.Now().Add(time.Hour)})

	w, _ := s.LaunchWorker(testKey, sessionsdomain.ThreadOptions{})
	_ = w.Send([]byte(`{"type":"resume_conversation","project_dir":"/proj","id":"conv-1"}`))
	frames := peer.received(t, 1)
	if !strings.Contains(string(frames[0]), `"/remote"`) {
		t.Fatalf("the relayed frame names %s", frames[0])
	}
	if l.count() != 0 {
		t.Fatal("a process worker was launched for a remote session")
	}
	w.Stop()
}
