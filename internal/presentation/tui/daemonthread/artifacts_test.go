package daemonthread

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// remoteThread connects a thread whose session the lease places on k8s-dev,
// under a home dir of the test's own.
func remoteThread(t *testing.T, d *fakeDaemon) *Thread {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	leases := storage.NewMemoryStorage()
	_, _, _ = leases.ClaimLease(t.Context(), agentdomain.Lease{ConversationID: "c1", Workspace: "k8s-dev", WorkspaceDir: "/ws/proj/c1", Until: time.Now().Add(time.Hour)})
	thread := New(d.config(), Options{ProjectDir: "/proj"}, leases)
	t.Cleanup(thread.Close)
	if err := thread.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := thread.Follow(t.Context(), "c1", sessionsdomain.ThreadOptions{}); err != nil {
		t.Fatal(err)
	}
	return thread
}

func TestArtifactsListsTheSessionsArtifactsFromItsWorkspace(t *testing.T) {
	d := startDaemon(t, 0)
	thread := remoteThread(t, d)
	d.answer("list_artifacts", `{"type":"artifacts","id":"c1","artifacts":[{"name":"shot.png","size":3,"modified_at":"2026-11-03T10:00:00Z"}]}`)

	listed, err := NewArtifacts(thread, d.config()).List(t.Context())

	if err != nil || len(listed) != 1 || listed[0].Name != "shot.png" || listed[0].Size != 3 || listed[0].ModifiedAt.IsZero() {
		t.Fatalf("List = %+v, %v", listed, err)
	}
	request := d.frames("list_artifacts")
	if len(request) != 1 || request[0]["id"] != "c1" || request[0]["project_dir"] != "/proj" {
		t.Fatalf("list_artifacts named %+v, want the session on its project", request)
	}
}

func TestArtifactsReportsTheWorkersError(t *testing.T) {
	d := startDaemon(t, 0)
	thread := remoteThread(t, d)
	d.answer("list_artifacts", `{"type":"artifacts","id":"c1","artifacts":[],"error":"permission denied"}`)

	_, err := NewArtifacts(thread, d.config()).List(t.Context())

	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("List err = %v, want the worker's error", err)
	}
}

func TestArtifactsFetchesARemoteArtifactIntoTheSessionsDirHere(t *testing.T) {
	d := startDaemon(t, 0)
	thread := remoteThread(t, d)
	cfg := d.config()
	d.answer("read_artifact", `{"type":"artifact","id":"c1","name":"shot.png","data":"cG5n"}`)

	path, err := NewArtifacts(thread, cfg).Fetch(t.Context(), "shot.png")

	if err != nil || path != filepath.Join(cfg.SessionArtifactsDir("c1"), "shot.png") {
		t.Fatalf("Fetch = %q, %v", path, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "png" {
		t.Fatalf("saved %q, %v, want the artifact's bytes", data, err)
	}
	request := d.frames("read_artifact")
	if len(request) != 1 || request[0]["name"] != "shot.png" || request[0]["id"] != "c1" {
		t.Fatalf("read_artifact named %+v", request)
	}
}

func TestArtifactsFetchRefusesANameOutsideTheSessionsDir(t *testing.T) {
	d := startDaemon(t, 0)
	thread := remoteThread(t, d)

	if _, err := NewArtifacts(thread, d.config()).Fetch(t.Context(), "../secret"); err == nil {
		t.Fatal("a traversal name was fetched")
	}
	if len(d.frames("read_artifact")) != 0 {
		t.Fatal("a refused name reached the daemon")
	}
}

func TestArtifactsFetchOnTheLocalWorkspaceIsTheFileHere(t *testing.T) {
	d := startDaemon(t, 0)
	t.Setenv("HOME", t.TempDir())
	thread, _ := connected(t, d)
	cfg := d.config()
	dir := cfg.SessionArtifactsDir(thread.Live())
	_ = os.MkdirAll(dir, 0o750)
	_ = os.WriteFile(filepath.Join(dir, "shot.png"), []byte("png"), 0o600)

	path, err := NewArtifacts(thread, cfg).Fetch(t.Context(), "shot.png")

	if err != nil || path != filepath.Join(dir, "shot.png") {
		t.Fatalf("Fetch = %q, %v", path, err)
	}
	if len(d.frames("read_artifact")) != 0 {
		t.Fatal("a local artifact was read over the binding")
	}
}
