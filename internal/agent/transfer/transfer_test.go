package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// handOverRequest is the request the handOver tests move, with its lease row.
func handOverRequest() (agentdomain.TransferRequest, agentdomain.Lease) {
	req := agentdomain.TransferRequest{
		TransferID: "t-1", Target: "remote",
		Session: agentdomain.Session{ProjectDir: "/proj", ConversationID: "conv-1", Slug: "proj"},
	}
	lease := agentdomain.Lease{ConversationID: "conv-1", Environment: "remote", Workspace: "/w", TransferID: "t-1"}
	return req, lease
}

func TestHandOverAbortsWhenTheFinalClaimRefuses(t *testing.T) {
	peer := startPeer(t, "remote", nil)
	conn, err := agui.Dial(t.Context(), agui.DialConfig{
		Addr: peer.endpoint().Addr, Token: "t", Kind: agentinfra.PeerClientKind, Handshake: testHandshake,
	}, newPeerFrames())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	leases := &agentdomainmocks.FakeLeaseStore{}
	leases.ClaimLeaseReturns(agentdomain.Lease{Environment: "k8s-dev"}, false, nil)
	s := New(Config{Identity: "laptop", WorkspaceRoot: t.TempDir()}, Deps{Leases: leases})
	client := &sessionsmocks.FakeClient{}
	req, lease := handOverRequest()

	s.handOver(t.Context(), conn, client, req, lease, peer.endpoint().Addr, nil)

	frames := peer.received(t, 1)
	var abort abortFrame
	if json.Unmarshal(frames[0], &abort) != nil || abort.Type != frameTransferAbort || abort.TransferID != "t-1" {
		t.Fatalf("the peer got %s, want transfer_abort for t-1", frames[0])
	}
	if client.DeliverCallCount() != 1 {
		t.Fatalf("the requester got %d frames, want only the failure", client.DeliverCallCount())
	}
	var progress progressFrame
	if err := json.Unmarshal(client.DeliverArgsForCall(0), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Type != frameTransferProgress || progress.Stage != agentdomain.TransferFailed ||
		!strings.Contains(progress.Message, "k8s-dev") || !strings.Contains(progress.Message, "recording the new owner") {
		t.Fatalf("progress = %+v, want the refusal naming the holder", progress)
	}
}

func TestHandOverAnnouncesAMoveItWon(t *testing.T) {
	leases := &agentdomainmocks.FakeLeaseStore{}
	leases.ClaimLeaseReturns(agentdomain.Lease{}, true, nil)
	s := New(Config{Identity: "laptop", WorkspaceRoot: t.TempDir()}, Deps{Leases: leases})
	client := &sessionsmocks.FakeClient{}
	req, lease := handOverRequest()

	s.handOver(t.Context(), nil, client, req, lease, "remote:1", json.RawMessage(`[]`))

	if client.DeliverCallCount() != 2 {
		t.Fatalf("the requester got %d frames, want ready and the move", client.DeliverCallCount())
	}
	var progress progressFrame
	if err := json.Unmarshal(client.DeliverArgsForCall(0), &progress); err != nil || progress.Stage != agentdomain.TransferReady {
		t.Fatalf("the first frame = %+v, want ready", progress)
	}
	var moved sessionMovedFrame
	if err := json.Unmarshal(client.DeliverArgsForCall(1), &moved); err != nil ||
		moved.Type != frameSessionMoved || moved.Environment != "remote" || moved.Workspace != "/w" {
		t.Fatalf("the second frame = %+v, want the move to remote", moved)
	}
}

func TestStartTransferRefusesASecondTransferOfTheSameSession(t *testing.T) {
	remote := fakeEnvironment("remote", agentdomain.EnvironmentKindDaemon)
	ensure := make(chan struct{})
	remote.EnsureCalls(func(context.Context, agentdomain.Session) (agentdomain.Endpoint, error) {
		<-ensure
		return agentdomain.Endpoint{}, errors.New("unreachable")
	})
	s, _ := newService(t, &launcher{}, remote)
	first, second := &sessionsmocks.FakeClient{}, &sessionsmocks.FakeClient{}
	frame := func(transferID string) clientFrame {
		return clientFrame{Type: frameTransferSession, TransferID: transferID, ProjectDir: "/proj", ID: "conv-1", Target: "remote"}
	}

	go s.startTransfer(first, frame("t-1"))
	eventually(t, "the first transfer to provision", func() bool { return remote.EnsureCallCount() == 1 })

	s.startTransfer(second, frame("t-2"))
	if remote.EnsureCallCount() != 1 {
		t.Fatal("the refused transfer touched the environment")
	}
	if second.DeliverCallCount() != 1 {
		t.Fatalf("the second requester got %d frames, want the refusal", second.DeliverCallCount())
	}
	var refusal progressFrame
	if err := json.Unmarshal(second.DeliverArgsForCall(0), &refusal); err != nil ||
		refusal.Stage != agentdomain.TransferFailed || refusal.TransferID != "t-2" ||
		!strings.Contains(refusal.Message, "a transfer of this session is already running") {
		t.Fatalf("refusal = %+v, want the session guard", refusal)
	}

	close(ensure)
	eventually(t, "the first transfer to fail", func() bool { return first.DeliverCallCount() == 2 })
	eventually(t, "the conversation guard to clear", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.conversations["conv-1"] == ""
	})

	go s.startTransfer(second, frame("t-3"))
	eventually(t, "a later transfer of the same session to run", func() bool { return remote.EnsureCallCount() == 2 })
}
