package infrastructure

import (
	"context"
	"errors"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// LocalWorkspace is this daemon as a workspace: sessions it owns run here, so
// it is never dialed, and it reports the daemon's own facts to list_workspaces.
type LocalWorkspace struct {
	name         string
	capabilities map[string]string
}

// NewLocalWorkspace names this daemon and what it reports about itself.
func NewLocalWorkspace(name string, capabilities map[string]string) *LocalWorkspace {
	return &LocalWorkspace{name: name, capabilities: capabilities}
}

func (e *LocalWorkspace) Name() string { return e.name }
func (e *LocalWorkspace) Kind() agentdomain.WorkspaceKind {
	return agentdomain.WorkspaceKindLocal
}

func (e *LocalWorkspace) Ensure(context.Context, agentdomain.Session) (agentdomain.Endpoint, error) {
	return agentdomain.Endpoint{}, errors.New("a session cannot be teleported to the daemon it already runs on")
}

func (e *LocalWorkspace) Reach(ctx context.Context, session agentdomain.Session) (agentdomain.Endpoint, error) {
	return e.Ensure(ctx, session)
}

func (e *LocalWorkspace) Probe(context.Context) agentdomain.WorkspaceStatus {
	return agentdomain.WorkspaceStatus{Name: e.name, Kind: agentdomain.WorkspaceKindLocal, Reachable: true, Capabilities: e.capabilities}
}

func (e *LocalWorkspace) Suspend(context.Context, agentdomain.Session) error { return nil }
func (e *LocalWorkspace) Delete(context.Context, agentdomain.Session) error  { return nil }
