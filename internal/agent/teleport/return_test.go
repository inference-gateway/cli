package teleport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessions "github.com/inference-gateway/cli/internal/sessions"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := utils.RunGit(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// checkout is a git checkout with one commit, the way a session leaves it.
func checkout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "-b", "main")
	for _, kv := range [][2]string{{"user.email", "test@example.com"}, {"user.name", "Test"}, {"commit.gpgsign", "false"}} {
		runGit(t, dir, "config", kv[0], kv[1])
	}
	runGit(t, dir, "commit", "--quiet", "--allow-empty", "-m", "init")
	return dir
}

// podCheckpoint is what the session's owner sends back after it committed
// on top of the checkout it got.
func podCheckpoint(t *testing.T, laptop string) agentdomain.Checkpoint {
	t.Helper()
	sent, err := agentinfra.PackWorkspace(t.Context(), laptop, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pod := filepath.Join(t.TempDir(), "pod")
	if err := agentinfra.UnpackWorkspace(t.Context(), sent, pod, nil); err != nil {
		t.Fatal(err)
	}
	for _, kv := range [][2]string{{"user.email", "pod@example.com"}, {"user.name", "Pod"}, {"commit.gpgsign", "false"}} {
		runGit(t, pod, "config", kv[0], kv[1])
	}
	runGit(t, pod, "commit", "--quiet", "--allow-empty", "-m", "done on the pod")
	workspace, err := agentinfra.PackWorkspace(t.Context(), pod, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return agentdomain.Checkpoint{
		SchemaVersion: agentdomain.CheckpointSchemaVersion,
		Session:       agentdomain.Session{ProjectDir: "/workspace/x", ConversationID: "conv-1", Slug: "x"},
		Digest:        "d-1",
		Conversation:  json.RawMessage(`{"entries":[],"metadata":{}}`),
		Workspace:     workspace,
	}
}

// importingLaunch launches workers that store any imported conversation and
// answer with the digest it renders to.
func importingLaunch(key sessionsdomain.ThreadKey, _ sessionsdomain.ThreadOptions) (sessionsdomain.Worker, error) {
	lines := make(chan []byte, 16)
	w := &sessionsmocks.FakeWorker{}
	w.LinesReturns(lines)
	w.StopCalls(sync.OnceFunc(func() { close(lines) }))
	w.SendCalls(func(frame []byte) error {
		if frameType(frame) == frameImportConversation {
			lines <- encode(map[string]string{"type": frameConversationImported, "id": key.ConversationID, "digest": "d-1"})
		}
		return nil
	})
	return w, nil
}

// returningService is the laptop daemon of a session that runs on k8s-dev,
// whose daemon answers teleport_return with checkpoint, its answer arriving
// only after the chunks.
func returningService(t *testing.T, checkpoint agentdomain.Checkpoint) (*Service, *storage.MemoryStorage, *peerBinding, *agentdomainmocks.FakeEnvironment) {
	t.Helper()
	peer := startPeer(t, "k8s-dev", func(conn *agui.Conn, frame []byte) {
		var f returnFrame
		if json.Unmarshal(frame, &f) == nil && f.Type == frameTeleportReturn {
			go func() {
				_ = sendCheckpoint(conn, f.TeleportID, f.Environment, checkpoint)
				_ = conn.Send(encode(returnFrame{Type: frameTeleportReturning, TeleportID: f.TeleportID}))
			}()
		}
	})
	environment := fakeEnvironment("k8s-dev", agentdomain.EnvironmentKindKubernetes)
	environment.ReachReturns(peer.endpoint(), nil)
	store := storage.NewMemoryStorage()
	s := New(Config{
		Identity: "laptop", WorkspaceRoot: t.TempDir(), StateDir: t.TempDir(),
		Entries: []config.EnvironmentEntry{{Name: "k8s-dev", Kind: config.EnvironmentKindKubernetes}},
	}, Deps{Leases: store, Environments: []agentdomain.Environment{environment}, Launch: importingLaunch})
	s.dial.Handshake = testHandshake
	s.AttachRegistry(sessions.NewRegistry(s.LaunchWorker, time.Minute))
	remote := agentdomain.Lease{ConversationID: "conv-1", Environment: "k8s-dev", Workspace: "/workspace/x", TeleportID: "t-0", Until: time.Now().Add(time.Hour)}
	if _, ok, err := store.ClaimLease(t.Context(), remote); err != nil || !ok {
		t.Fatalf("seeding the remote lease: %v", err)
	}
	return s, store, peer, environment
}

func requestTeleportBack(s *Service, laptop string) *sessionsmocks.FakeClient {
	client := &sessionsmocks.FakeClient{}
	s.startTeleport(client, clientFrame{Type: frameTeleportSession, TeleportID: "t-1", ProjectDir: laptop, ID: "conv-1", Target: "laptop"})
	return client
}

func delivered[T any](client *sessionsmocks.FakeClient, frameType string) []T {
	var out []T
	for i := range client.DeliverCallCount() {
		frame := client.DeliverArgsForCall(i)
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(frame, &head) != nil || head.Type != frameType {
			continue
		}
		var v T
		if json.Unmarshal(frame, &v) == nil {
			out = append(out, v)
		}
	}
	return out
}

func TestBringBackLandsTheSessionAndTakesItsLeaseBack(t *testing.T) {
	laptop := checkout(t)
	s, store, peer, _ := returningService(t, podCheckpoint(t, laptop))

	client := requestTeleportBack(s, laptop)

	if progress := delivered[progressFrame](client, frameTeleportProgress); len(progress) == 0 || progress[len(progress)-1].Stage != agentdomain.TeleportReady {
		t.Fatalf("progress = %+v, want it to end ready", progress)
	}
	teleported := delivered[sessionTeleportedFrame](client, frameSessionTeleported)
	if len(teleported) != 1 || teleported[0].Environment != "laptop" || teleported[0].Workspace != laptop {
		t.Fatalf("teleports = %+v, want one back to the laptop checkout", teleported)
	}
	lease, found, err := store.GetLease(t.Context(), "conv-1")
	if err != nil || !found || lease.Environment != "laptop" || lease.Workspace != laptop {
		t.Fatalf("lease = %+v, %v, %v, want the laptop's own", lease, found, err)
	}
	if got := runGit(t, laptop, "log", "-1", "--format=%s"); got != "done on the pod" {
		t.Errorf("the checkout's HEAD is %q, want the pod's commit", got)
	}
	frames := peer.received(t, 2)
	if frameType(frames[0]) != frameTeleportReturn || frameType(frames[1]) != frameTeleportReturned {
		t.Errorf("k8s-dev got %s then %s, want the return request and its confirmation", frames[0], frames[1])
	}
}

func TestBringBackRefusesACheckoutThatChangedSinceItLeft(t *testing.T) {
	laptop := checkout(t)
	s, store, _, environment := returningService(t, podCheckpoint(t, laptop))
	runGit(t, laptop, "commit", "--quiet", "--allow-empty", "-m", "edited after the teleport")
	if err := os.WriteFile(filepath.Join(laptop, "draft.txt"), []byte("draft"), 0o644); err != nil {
		t.Fatal(err)
	}

	client := requestTeleportBack(s, laptop)

	progress := delivered[progressFrame](client, frameTeleportProgress)
	if len(progress) != 1 || progress[0].Stage != agentdomain.TeleportFailed || !strings.Contains(progress[0].Message, "changed since the session left") {
		t.Fatalf("progress = %+v, want one refusal naming the changed checkout", progress)
	}
	if environment.ReachCallCount() != 0 {
		t.Error("the refused teleport reached k8s-dev")
	}
	if lease, _, _ := store.GetLease(t.Context(), "conv-1"); lease.Environment != "k8s-dev" {
		t.Errorf("the refused teleport left the lease with %s", lease.Environment)
	}
}

func TestBringBackAcceptsADirtyCheckoutUnchangedSinceItLeft(t *testing.T) {
	laptop := checkout(t)
	if err := os.WriteFile(filepath.Join(laptop, "draft.txt"), []byte("draft"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkpoint := podCheckpoint(t, laptop)
	s, _, _, _ := returningService(t, checkpoint)
	fingerprint, _, err := agentinfra.CheckoutState(t.Context(), laptop, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.rememberOrigin("conv-1", laptop, fingerprint)

	client := requestTeleportBack(s, laptop)

	if teleported := delivered[sessionTeleportedFrame](client, frameSessionTeleported); len(teleported) != 1 {
		t.Fatalf("progress = %+v, want the session back", delivered[progressFrame](client, frameTeleportProgress))
	}
	if left, _ := s.origin("conv-1"); left != "" {
		t.Error("the origin of a session that came back is still remembered")
	}
}

func TestPackForReturnRefusesWhatItDidNotRestore(t *testing.T) {
	tests := []struct {
		name      string
		owner     string
		workspace func(root string) string
		wantErr   string
	}{
		{name: "another owner", owner: "k8s-other", workspace: func(root string) string { return filepath.Join(root, "x", "conv-1") }, wantErr: "runs on k8s-other"},
		{name: "outside the workspace root", workspace: func(string) string { return "/etc" }, wantErr: "is not a workspace"},
		{name: "no workspace", workspace: func(string) string { return "" }, wantErr: "is not a workspace"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := startReceiver(t, agentinfra.PeerClientKind)
			if tt.owner != "" {
				_, _, _ = r.service.leases.ClaimLease(t.Context(), agentdomain.Lease{ConversationID: "conv-1", Environment: tt.owner, Until: time.Now().Add(time.Hour)})
			}

			r.send(t, returnFrame{Type: frameTeleportReturn, TeleportID: "t-1", ID: "conv-1", Workspace: tt.workspace(r.service.cfg.WorkspaceRoot), Environment: "home"})

			ctx, cancel := contextWithTimeout(t, 5*time.Second)
			defer cancel()
			if _, err := r.frames.await(ctx, frameTeleportReturning); err != nil {
				t.Fatalf("no answer to the teleport back: %v", err)
			}
			if _, err := r.frames.await(ctx, frameTeleportChunk); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("await = %v, want a refusal containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestOriginFileTakesOnlyPlainConversationIDs(t *testing.T) {
	s := New(Config{Identity: "laptop", StateDir: t.TempDir()}, Deps{})

	for _, id := range []string{"", ".", "..", "a/b", "../escape", "x/../y"} {
		if path := s.originFile(id); path != "" {
			t.Errorf("originFile(%q) = %q, want none", id, path)
		}
	}
	if path := s.originFile("conv-1"); path != filepath.Join(s.cfg.StateDir, "origins", "conv-1") {
		t.Errorf("originFile(conv-1) = %q", path)
	}
}

func TestStartTeleportRefusesAConversationIDThatIsNotAPlainName(t *testing.T) {
	s, _ := newService(t, &launcher{})
	client := &sessionsmocks.FakeClient{}

	s.startTeleport(client, clientFrame{Type: frameTeleportSession, TeleportID: "t-1", ProjectDir: "/proj", ID: "../../etc", Target: "laptop"})

	progress := delivered[progressFrame](client, frameTeleportProgress)
	if len(progress) != 1 || progress[0].Stage != agentdomain.TeleportFailed || !strings.Contains(progress[0].Message, "not a plain name") {
		t.Fatalf("progress = %+v, want the id refused", progress)
	}
}

func TestBringBackRefusesAnotherCheckoutThanTheOneItLeft(t *testing.T) {
	laptop := checkout(t)
	s, store, _, environment := returningService(t, podCheckpoint(t, laptop))
	s.rememberOrigin("conv-1", "/somewhere/else", "fingerprint")

	client := requestTeleportBack(s, laptop)

	progress := delivered[progressFrame](client, frameTeleportProgress)
	if len(progress) != 1 || progress[0].Stage != agentdomain.TeleportFailed || !strings.Contains(progress[0].Message, "/somewhere/else") {
		t.Fatalf("progress = %+v, want a refusal naming the checkout the session left", progress)
	}
	if environment.ReachCallCount() != 0 {
		t.Error("the refused teleport reached k8s-dev")
	}
	if lease, _, _ := store.GetLease(t.Context(), "conv-1"); lease.Environment != "k8s-dev" {
		t.Errorf("the refused teleport left the lease with %s", lease.Environment)
	}
}
