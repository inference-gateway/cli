package daemonthread

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	config "github.com/inference-gateway/cli/config"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	projects "github.com/inference-gateway/cli/internal/projects"
)

// Artifacts browses the session's artifacts in daemon mode: the worker of
// the workspace that owns the session lists and reads them, and a remote
// one's land in this machine's artifacts dir for the session.
type Artifacts struct {
	thread *Thread
	cfg    *config.Config
}

// NewArtifacts builds the daemon-mode artifact browser over thread.
func NewArtifacts(thread *Thread, cfg *config.Config) *Artifacts {
	return &Artifacts{thread: thread, cfg: cfg}
}

// List asks the owning workspace's worker for the session's artifacts.
func (a *Artifacts) List(ctx context.Context) ([]tui.Artifact, error) {
	frame := map[string]string{"type": "list_artifacts", "project_dir": a.thread.projectDir(), "id": a.thread.Live()}
	data, err := a.thread.Request(ctx, frame, "artifacts")
	if err != nil {
		return nil, err
	}
	var listed struct {
		Artifacts []struct {
			Name       string    `json:"name"`
			Size       int64     `json:"size"`
			ModifiedAt time.Time `json:"modified_at"`
		} `json:"artifacts"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &listed); err != nil {
		return nil, err
	}
	if listed.Error != "" {
		return nil, errors.New(listed.Error)
	}
	out := make([]tui.Artifact, 0, len(listed.Artifacts))
	for _, item := range listed.Artifacts {
		out = append(out, tui.Artifact{Name: item.Name, Size: item.Size, ModifiedAt: item.ModifiedAt})
	}
	return out, nil
}

// Fetch is the artifact's path on this machine: its own when the session
// runs here, else a copy read from the owning workspace.
func (a *Artifacts) Fetch(ctx context.Context, name string) (string, error) {
	path, err := projects.ArtifactPath(a.cfg.SessionArtifactsDir(a.thread.Live()), name)
	if err != nil {
		return "", err
	}
	if !a.thread.Placement().Remote() {
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return path, nil
	}
	data, err := a.read(ctx, name)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}

func (a *Artifacts) read(ctx context.Context, name string) ([]byte, error) {
	frame := map[string]string{"type": "read_artifact", "project_dir": a.thread.projectDir(), "id": a.thread.Live(), "name": name}
	data, err := a.thread.Request(ctx, frame, "artifact")
	if err != nil {
		return nil, err
	}
	var reply struct {
		Data  string `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &reply); err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New(reply.Error)
	}
	return base64.StdEncoding.DecodeString(reply.Data)
}
