package teleport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	aguimocks "github.com/inference-gateway/cli/tests/mocks/agui"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// receiver is a service behind an in-test binding, dialed as a daemon.
type receiver struct {
	service *Service
	conn    *agui.Conn
	frames  *peerFrames
}

func startReceiver(t *testing.T, kind string) *receiver {
	t.Helper()
	s, _ := newService(t, &launcher{})
	handler := &aguimocks.FakeHandler{}
	handler.HandleCalls(func(conn *agui.Conn, frame []byte) { s.Handle(conn, frame) })
	binding := agui.NewBinding(agui.BindingConfig{Token: "t", Handshake: testHandshake}, handler)
	if err := binding.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(binding.Close)
	frames := newPeerFrames()
	conn, err := agui.Dial(t.Context(), agui.DialConfig{Addr: binding.Addr(), Token: "t", Kind: kind, Handshake: testHandshake}, frames)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	return &receiver{service: s, conn: conn, frames: frames}
}

func (r *receiver) send(t *testing.T, frame any) {
	t.Helper()
	if err := r.conn.Send(encode(frame)); err != nil {
		t.Fatal(err)
	}
}

func (r *receiver) importCheckpoint(t *testing.T, checkpoint agentdomain.Checkpoint, workspace string, tamper func(payload []byte) []byte) importedFrame {
	t.Helper()
	payload := encode(checkpoint)
	sum := sha256.Sum256(payload)
	if tamper != nil {
		payload = tamper(payload)
	}
	r.send(t, chunkFrame{Type: frameTeleportChunk, TeleportID: "t-1", Seq: 0, Data: payload[:len(payload)/2]})
	r.send(t, chunkFrame{Type: frameTeleportChunk, TeleportID: "t-1", Seq: 1, Data: payload[len(payload)/2:]})
	r.send(t, importFrame{Type: frameTeleportImport, TeleportID: "t-1", Workspace: workspace, Chunks: 2, SHA256: hex.EncodeToString(sum[:])})
	ctx, cancel := contextWithTimeout(t, 5*time.Second)
	defer cancel()
	reply, err := r.frames.await(ctx, frameTeleportImported)
	if err != nil {
		t.Fatalf("no teleport_imported: %v", err)
	}
	var imported importedFrame
	_ = json.Unmarshal(reply, &imported)
	return imported
}

func TestReceiveValidatesTheCheckpointBeforeRestoring(t *testing.T) {
	checkpoint := agentdomain.Checkpoint{SchemaVersion: agentdomain.CheckpointSchemaVersion, Session: agentdomain.Session{ConversationID: "conv-1", Slug: "proj"}}
	tests := []struct {
		name       string
		checkpoint agentdomain.Checkpoint
		workspace  string
		tamper     func([]byte) []byte
		wantErr    string
	}{
		{"wrong digest", checkpoint, "laptop", func(p []byte) []byte { return append(p, ' ') }, "digest"},
		{"wrong workspace", checkpoint, "someone-else", nil, "not \"someone-else\""},
		{"wrong schema", agentdomain.Checkpoint{SchemaVersion: 99, Session: checkpoint.Session}, "laptop", nil, "schema 99"},
		{"empty bundle", checkpoint, "laptop", nil, "restoring the repository"},
		{"slug leaves the projects root", agentdomain.Checkpoint{SchemaVersion: checkpoint.SchemaVersion, Session: agentdomain.Session{ConversationID: "conv-1", Slug: ".."}}, "laptop", nil, "not a plain name"},
		{"id leaves the projects root", agentdomain.Checkpoint{SchemaVersion: checkpoint.SchemaVersion, Session: agentdomain.Session{ConversationID: "../../outside", Slug: "proj"}}, "laptop", nil, "not a plain name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := startReceiver(t, agentinfra.PeerClientKind)
			imported := r.importCheckpoint(t, tt.checkpoint, tt.workspace, tt.tamper)
			if imported.OK || !strings.Contains(imported.Error, tt.wantErr) {
				t.Fatalf("imported = %+v, want an error containing %q", imported, tt.wantErr)
			}
		})
	}
}

func TestReceiveIgnoresPeerFramesFromClients(t *testing.T) {
	r := startReceiver(t, "desktop")
	r.send(t, importFrame{Type: frameTeleportImport, TeleportID: "t-1", Workspace: "laptop", Chunks: 0})
	ctx, cancel := contextWithTimeout(t, 300*time.Millisecond)
	defer cancel()
	if _, err := r.frames.await(ctx, frameTeleportImported); err == nil {
		t.Fatal("a client's teleport_import was answered")
	}
}

func TestTeleportSessionNeedsARemoteTarget(t *testing.T) {
	r := startReceiver(t, "desktop")
	r.send(t, map[string]string{"type": frameTeleportSession, "teleport_id": "t-1", "project_dir": "/proj", "id": "conv-1", "target": "nowhere"})
	ctx, cancel := contextWithTimeout(t, 5*time.Second)
	defer cancel()
	reply, err := r.frames.await(ctx, frameTeleportProgress)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(reply), `"stage":"failed"`) || !strings.Contains(string(reply), "nowhere") {
		t.Fatalf("progress = %s", reply)
	}
	r.send(t, map[string]string{"type": frameTeleportSession, "teleport_id": "", "project_dir": "/proj", "id": "conv-1", "target": "x"})
	if _, err := r.frames.await(ctx, "never"); err == nil || !strings.Contains(err.Error(), "needs teleport_id") {
		t.Fatalf("incomplete request = %v", err)
	}
}

func TestListWorkspacesReportsTheLocalWorkspaceFirst(t *testing.T) {
	r := startReceiver(t, "desktop")
	r.service.workspaces["local"] = agentinfra.NewLocalWorkspace("laptop", map[string]string{"version": "test"})
	unreachable := fakeWorkspace("k8s", agentdomain.WorkspaceKindKubernetes)
	unreachable.ProbeReturns(agentdomain.WorkspaceStatus{Name: "k8s", Kind: agentdomain.WorkspaceKindKubernetes, Error: "no cluster"})
	r.service.workspaces["k8s"] = unreachable
	r.send(t, map[string]string{"type": frameListWorkspaces})
	ctx, cancel := contextWithTimeout(t, 5*time.Second)
	defer cancel()
	reply, err := r.frames.await(ctx, frameWorkspaces)
	if err != nil {
		t.Fatal(err)
	}
	var listed workspacesFrame
	_ = json.Unmarshal(reply, &listed)
	if len(listed.Workspaces) != 2 || listed.Workspaces[0].Name != "laptop" || !listed.Workspaces[0].Reachable {
		t.Fatalf("workspaces = %+v", listed.Workspaces)
	}
	if listed.Workspaces[1].Reachable || listed.Workspaces[1].Error != "no cluster" {
		t.Fatalf("an unreachable workspace must be reported with its error: %+v", listed.Workspaces[1])
	}
}
