package headless

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testMaxArtifact = 1 << 20

func artifactsPanel(t *testing.T) (*Panel, lineSink, string) {
	t.Helper()
	root := t.TempDir()
	deps := panelDeps()
	deps.ArtifactsDir = func(sessionID string) string { return filepath.Join(root, sessionID) }
	deps.MediaDir = func() string { return filepath.Join(root, "media") }
	deps.MaxArtifact = testMaxArtifact
	p, sink := startPanel(deps)
	return p, sink, root
}

func TestPanelListsSessionArtifactsNewestFirst(t *testing.T) {
	p, sink, root := artifactsPanel(t)
	dir := filepath.Join(root, "s1")
	_ = os.MkdirAll(filepath.Join(dir, "nested"), 0o750)
	old := filepath.Join(dir, "old.png")
	_ = os.WriteFile(old, []byte("old"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "new.gif"), []byte("newer"), 0o600)
	_ = os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))

	if !handle(t, p, map[string]any{"type": "list_artifacts", "id": "s1"}) {
		t.Fatal("list_artifacts must be consumed")
	}
	frame := readFrame(t, sink, "artifacts")
	listed := frame["artifacts"].([]any)
	if frame["id"] != "s1" || len(listed) != 2 {
		t.Fatalf("unexpected reply %v", frame)
	}
	first := listed[0].(map[string]any)
	if first["name"] != "new.gif" || first["size"] != float64(5) || first["modified_at"] == nil {
		t.Fatalf("unexpected first artifact %v", first)
	}
}

func TestPanelListsNoArtifactsForAFreshSession(t *testing.T) {
	p, sink, _ := artifactsPanel(t)
	handle(t, p, map[string]any{"type": "list_artifacts", "id": "fresh"})
	frame := readFrame(t, sink, "artifacts")
	if len(frame["artifacts"].([]any)) != 0 || frame["error"] != nil {
		t.Fatalf("unexpected reply %v", frame)
	}
}

func TestPanelReadsAnArtifact(t *testing.T) {
	p, sink, root := artifactsPanel(t)
	_ = os.MkdirAll(filepath.Join(root, "s1"), 0o750)
	_ = os.WriteFile(filepath.Join(root, "s1", "shot.png"), []byte("png-bytes"), 0o600)

	if !handle(t, p, map[string]any{"type": "read_artifact", "id": "s1", "name": "shot.png"}) {
		t.Fatal("read_artifact must be consumed")
	}
	frame := readFrame(t, sink, "artifact")
	if frame["name"] != "shot.png" || frame["data"] != base64.StdEncoding.EncodeToString([]byte("png-bytes")) {
		t.Fatalf("unexpected reply %v", frame)
	}
}

func TestPanelReadsAMediaFileByItsAbsolutePath(t *testing.T) {
	p, sink, root := artifactsPanel(t)
	wav := filepath.Join(root, "media", "tts", "line-01.wav")
	_ = os.MkdirAll(filepath.Dir(wav), 0o750)
	_ = os.WriteFile(wav, []byte("wav-bytes"), 0o600)

	handle(t, p, map[string]any{"type": "read_artifact", "id": "s1", "name": wav})
	frame := readFrame(t, sink, "artifact")
	if frame["name"] != wav || frame["data"] != base64.StdEncoding.EncodeToString([]byte("wav-bytes")) {
		t.Fatalf("unexpected reply %v", frame)
	}
}

func TestPanelRefusesAnArtifactOutsideTheSessionDir(t *testing.T) {
	p, sink, root := artifactsPanel(t)
	_ = os.WriteFile(filepath.Join(root, "secret"), []byte("x"), 0o600)
	for _, name := range []string{"../secret", "", "a/b", filepath.Join(root, "secret"), filepath.Join(root, "media", "..", "secret")} {
		handle(t, p, map[string]any{"type": "read_artifact", "id": "s1", "name": name})
		frame := readFrame(t, sink, "artifact")
		if frame["error"] == nil || frame["data"] != nil {
			t.Fatalf("%q must be refused, got %v", name, frame)
		}
	}
}

func TestPanelRefusesAnArtifactOverTheFrameLimit(t *testing.T) {
	p, sink, root := artifactsPanel(t)
	_ = os.MkdirAll(filepath.Join(root, "s1"), 0o750)
	big, err := os.Create(filepath.Join(root, "s1", "big.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if err := big.Truncate(testMaxArtifact + 1); err != nil {
		t.Fatal(err)
	}
	_ = big.Close()
	handle(t, p, map[string]any{"type": "read_artifact", "id": "s1", "name": "big.mp4"})
	frame := readFrame(t, sink, "artifact")
	if frame["error"] == nil || frame["data"] != nil {
		t.Fatalf("an oversized artifact must be refused, got %v", frame)
	}
	if msg, _ := frame["error"].(string); !strings.Contains(msg, "daemon.max_artifact_mb") {
		t.Fatalf("the refusal must name the setting that raises the cap, got %q", msg)
	}
}
