package projects

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ErrArtifactName refuses a name that is not one file directly under the
// session's artifacts dir.
var ErrArtifactName = errors.New("an artifact name is one file name under the session's artifacts dir")

// Artifact is one file a session saved under its artifacts dir.
type Artifact struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// ListArtifacts lists the files directly under dir, newest first. A missing
// dir lists as empty, since a session may have saved nothing yet.
func ListArtifacts(dir string) ([]Artifact, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Artifact{}, nil
	}
	if err != nil {
		return nil, err
	}
	artifacts := make([]Artifact, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.IsDir() {
			continue
		}
		artifacts = append(artifacts, Artifact{Name: entry.Name(), Size: info.Size(), ModifiedAt: info.ModTime()})
	}
	slices.SortFunc(artifacts, func(a, b Artifact) int {
		return cmp.Or(b.ModifiedAt.Compare(a.ModifiedAt), cmp.Compare(a.Name, b.Name))
	})
	return artifacts, nil
}

// ArtifactPath is the path of the artifact name under dir, refusing a name
// that names anything but one file directly under it.
func ArtifactPath(dir, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || !filepath.IsLocal(name) {
		return "", ErrArtifactName
	}
	return filepath.Join(dir, name), nil
}
