package teleport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// handOverRequest is the request the handOver tests teleport, with its lease row.
func handOverRequest() (agentdomain.TeleportRequest, agentdomain.Lease) {
	req := agentdomain.TeleportRequest{
		TeleportID: "t-1", Target: "remote",
		Session: agentdomain.Session{ProjectDir: "/proj", ConversationID: "conv-1", Slug: "proj"},
	}
	lease := agentdomain.Lease{ConversationID: "conv-1", Workspace: "remote", WorkspaceDir: "/w", TeleportID: "t-1"}
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
	leases.ClaimLeaseReturns(agentdomain.Lease{Workspace: "k8s-dev"}, false, nil)
	s := New(Config{Identity: "laptop", ProjectsRoot: t.TempDir()}, Deps{Leases: leases})
	client := &sessionsmocks.FakeClient{}
	req, lease := handOverRequest()

	s.handOver(t.Context(), conn, client, req, lease, peer.endpoint().Addr, nil)

	frames := peer.received(t, 1)
	var abort abortFrame
	if json.Unmarshal(frames[0], &abort) != nil || abort.Type != frameTeleportAbort || abort.TeleportID != "t-1" {
		t.Fatalf("the peer got %s, want teleport_abort for t-1", frames[0])
	}
	if client.DeliverCallCount() != 1 {
		t.Fatalf("the requester got %d frames, want only the failure", client.DeliverCallCount())
	}
	var progress progressFrame
	if err := json.Unmarshal(client.DeliverArgsForCall(0), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Type != frameTeleportProgress || progress.Stage != agentdomain.TeleportFailed ||
		!strings.Contains(progress.Message, "k8s-dev") || !strings.Contains(progress.Message, "recording the new owner") {
		t.Fatalf("progress = %+v, want the refusal naming the holder", progress)
	}
}

func TestHandOverAnnouncesATeleportItWon(t *testing.T) {
	leases := &agentdomainmocks.FakeLeaseStore{}
	leases.ClaimLeaseReturns(agentdomain.Lease{}, true, nil)
	s := New(Config{Identity: "laptop", ProjectsRoot: t.TempDir()}, Deps{Leases: leases})
	client := &sessionsmocks.FakeClient{}
	req, lease := handOverRequest()

	s.handOver(t.Context(), nil, client, req, lease, "remote:1", json.RawMessage(`[]`))

	if client.DeliverCallCount() != 2 {
		t.Fatalf("the requester got %d frames, want ready and the teleport", client.DeliverCallCount())
	}
	var progress progressFrame
	if err := json.Unmarshal(client.DeliverArgsForCall(0), &progress); err != nil || progress.Stage != agentdomain.TeleportReady {
		t.Fatalf("the first frame = %+v, want ready", progress)
	}
	var teleported sessionTeleportedFrame
	if err := json.Unmarshal(client.DeliverArgsForCall(1), &teleported); err != nil ||
		teleported.Type != frameSessionTeleported || teleported.Workspace != "remote" || teleported.WorkspaceDir != "/w" {
		t.Fatalf("the second frame = %+v, want the teleport to remote", teleported)
	}
}

func TestTeleportFailingAfterProvisioningDiscardsTheTarget(t *testing.T) {
	peer := startPeer(t, "k8s-desktop", nil)
	target := fakeWorkspace("k8s-dev", agentdomain.WorkspaceKindKubernetes)
	s, store := newService(t, &launcher{}, target)
	s.dial.Handshake = testHandshake
	req, _ := handOverRequest()
	req.Target = "k8s-dev"
	target.EnsureCalls(func(ctx context.Context, _ agentdomain.Session) (agentdomain.Endpoint, error) {
		claim := agentdomain.Lease{ConversationID: "conv-1", Workspace: "k8s-dev", TeleportID: req.TeleportID, Until: time.Now().Add(time.Hour)}
		if _, ok, err := store.ClaimLease(ctx, claim); err != nil || !ok {
			t.Errorf("the target's claim = %v, %v", ok, err)
		}
		return peer.endpoint(), nil
	})
	client := &sessionsmocks.FakeClient{}

	s.teleport(t.Context(), client, req)

	var last progressFrame
	if err := json.Unmarshal(client.DeliverArgsForCall(client.DeliverCallCount()-1), &last); err != nil ||
		last.Stage != agentdomain.TeleportFailed || !strings.Contains(last.Message, "identifies as") {
		t.Fatalf("last frame = %+v, want the identity failure", last)
	}
	if target.DeleteCallCount() != 1 {
		t.Fatalf("the target's runtime was deleted %d times, want once", target.DeleteCallCount())
	}
	if lease, found, _ := store.GetLease(t.Context(), "conv-1"); found && lease.Workspace == "k8s-dev" {
		t.Fatalf("the failed target still holds the lease: %+v", lease)
	}
}

func TestStartTeleportRefusesASecondTeleportOfTheSameSession(t *testing.T) {
	remote := fakeWorkspace("remote", agentdomain.WorkspaceKindDaemon)
	ensure := make(chan struct{})
	remote.EnsureCalls(func(context.Context, agentdomain.Session) (agentdomain.Endpoint, error) {
		<-ensure
		return agentdomain.Endpoint{}, errors.New("unreachable")
	})
	s, _ := newService(t, &launcher{}, remote)
	first, second := &sessionsmocks.FakeClient{}, &sessionsmocks.FakeClient{}
	frame := func(teleportID string) clientFrame {
		return clientFrame{Type: frameTeleportSession, TeleportID: teleportID, ProjectDir: "/proj", ID: "conv-1", Target: "remote"}
	}

	go s.startTeleport(first, frame("t-1"))
	eventually(t, "the first teleport to provision", func() bool { return remote.EnsureCallCount() == 1 })

	s.startTeleport(second, frame("t-2"))
	if remote.EnsureCallCount() != 1 {
		t.Fatal("the refused teleport touched the workspace")
	}
	if second.DeliverCallCount() != 1 {
		t.Fatalf("the second requester got %d frames, want the refusal", second.DeliverCallCount())
	}
	var refusal progressFrame
	if err := json.Unmarshal(second.DeliverArgsForCall(0), &refusal); err != nil ||
		refusal.Stage != agentdomain.TeleportFailed || refusal.TeleportID != "t-2" ||
		!strings.Contains(refusal.Message, "a teleport of this session is already running") {
		t.Fatalf("refusal = %+v, want the session guard", refusal)
	}

	close(ensure)
	eventually(t, "the first teleport to fail", func() bool { return first.DeliverCallCount() == 2 })
	eventually(t, "the conversation guard to clear", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.conversations["conv-1"] == ""
	})

	go s.startTeleport(second, frame("t-3"))
	eventually(t, "a later teleport of the same session to run", func() bool { return remote.EnsureCallCount() == 2 })
}
