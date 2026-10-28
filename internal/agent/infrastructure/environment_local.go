package infrastructure

import (
	"context"
	"errors"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// LocalEnvironment is this daemon as an environment: sessions it owns run here, so
// it is never dialed, and it reports the daemon's own facts to list_environments.
type LocalEnvironment struct {
	name         string
	capabilities map[string]string
}

// NewLocalEnvironment names this daemon and what it reports about itself.
func NewLocalEnvironment(name string, capabilities map[string]string) *LocalEnvironment {
	return &LocalEnvironment{name: name, capabilities: capabilities}
}

func (e *LocalEnvironment) Name() string { return e.name }
func (e *LocalEnvironment) Kind() agentdomain.EnvironmentKind {
	return agentdomain.EnvironmentKindLocal
}

func (e *LocalEnvironment) Ensure(context.Context, agentdomain.Session) (agentdomain.Endpoint, error) {
	return agentdomain.Endpoint{}, errors.New("a session cannot be transferred to the daemon it already runs on")
}

func (e *LocalEnvironment) Reach(ctx context.Context, session agentdomain.Session) (agentdomain.Endpoint, error) {
	return e.Ensure(ctx, session)
}

func (e *LocalEnvironment) Probe(context.Context) agentdomain.EnvironmentStatus {
	return agentdomain.EnvironmentStatus{Name: e.name, Kind: agentdomain.EnvironmentKindLocal, Reachable: true, Capabilities: e.capabilities}
}

func (e *LocalEnvironment) Suspend(context.Context, agentdomain.Session) error { return nil }
func (e *LocalEnvironment) Delete(context.Context, agentdomain.Session) error  { return nil }
