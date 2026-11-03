package headless

import (
	"encoding/base64"
	"fmt"
	"os"

	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	projects "github.com/inference-gateway/cli/internal/projects"
)

// The panel frames the chat TUI browses a session's artifacts with, answered
// by the worker of the workspace that owns the session.
const (
	inboundListArtifacts = "list_artifacts"
	inboundReadArtifact  = "read_artifact"

	outboundArtifacts = "artifacts"
	outboundArtifact  = "artifact"
)

// artifactsFrame answers list_artifacts with the session's files, newest first.
type artifactsFrame struct {
	Type      string              `json:"type"`
	ID        string              `json:"id"`
	Artifacts []projects.Artifact `json:"artifacts"`
	Error     string              `json:"error,omitempty"`
}

// artifactFrame answers read_artifact with the file's bytes in base64.
type artifactFrame struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Data  string `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// artifacts answers the artifact frames from the session's artifacts dir in
// this worker's project.
type artifacts struct {
	write frameWriter
	dir   func(sessionID string) string
}

func (a *artifacts) list(id string) {
	listed, err := projects.ListArtifacts(a.dir(id))
	if err != nil {
		a.write(artifactsFrame{Type: outboundArtifacts, ID: id, Artifacts: []projects.Artifact{}, Error: err.Error()})
		return
	}
	a.write(artifactsFrame{Type: outboundArtifacts, ID: id, Artifacts: listed})
}

// read answers with the artifact's bytes. ponytail: one base64 frame capped at
// the binding's frame limit, chunk like teleport_chunk when recordings outgrow it.
func (a *artifacts) read(id, name string) {
	data, err := a.load(id, name)
	if err != nil {
		a.write(artifactFrame{Type: outboundArtifact, ID: id, Name: name, Error: err.Error()})
		return
	}
	a.write(artifactFrame{Type: outboundArtifact, ID: id, Name: name, Data: base64.StdEncoding.EncodeToString(data)})
}

func (a *artifacts) load(id, name string) ([]byte, error) {
	path, err := projects.ArtifactPath(a.dir(id), name)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, projects.ErrArtifactName
	}
	if info.Size() > projects.MaxArtifactFrameBytes {
		return nil, fmt.Errorf("%s is %s, over the %s a frame carries", name, formatting.FormatBytes(info.Size()), formatting.FormatBytes(projects.MaxArtifactFrameBytes))
	}
	return os.ReadFile(path)
}
