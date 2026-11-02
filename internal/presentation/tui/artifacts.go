package tui

import (
	"context"
	"os"
	"time"

	projects "github.com/inference-gateway/cli/internal/projects"
)

// Artifact is one file the session saved under its artifacts dir, on the
// workspace it runs on.
type Artifact struct {
	Name       string
	Size       int64
	ModifiedAt time.Time
}

// ArtifactBrowser lists the current session's artifacts from the workspace
// that owns it and fetches one to this machine, for /artifacts and the
// workspace indicator.
type ArtifactBrowser interface {
	List(ctx context.Context) ([]Artifact, error)
	Fetch(ctx context.Context, name string) (string, error)
}

// LocalArtifactBrowser browses the artifacts of an in-process chat, which
// are on this machine already, under the dir Dir names at call time.
type LocalArtifactBrowser struct {
	Dir func() string
}

func (b LocalArtifactBrowser) List(context.Context) ([]Artifact, error) {
	listed, err := projects.ListArtifacts(b.Dir())
	if err != nil {
		return nil, err
	}
	out := make([]Artifact, 0, len(listed))
	for _, a := range listed {
		out = append(out, Artifact{Name: a.Name, Size: a.Size, ModifiedAt: a.ModifiedAt})
	}
	return out, nil
}

// Fetch is the artifact's path here, once it is checked to exist.
func (b LocalArtifactBrowser) Fetch(_ context.Context, name string) (string, error) {
	path, err := projects.ArtifactPath(b.Dir(), name)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

// ShowArtifactsEvent opens the session's artifacts view.
type ShowArtifactsEvent struct{}

// ArtifactsLoadedEvent carries the session's artifacts, or why they could
// not be listed.
type ArtifactsLoadedEvent struct {
	Artifacts []Artifact
	Err       error
}

// ArtifactChosenEvent asks to save the artifact Name here, and to open it
// with the system viewer when Open is set.
type ArtifactChosenEvent struct {
	Name string
	Open bool
}

// ArtifactSavedEvent reports where the artifact landed, or why it did not.
type ArtifactSavedEvent struct {
	Name string
	Path string
	Open bool
	Err  error
}
