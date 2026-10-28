package infrastructure

import (
	"context"
	"errors"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// LocalEnvironment is this daemon as an environment: sessions it owns run here, so
// it is never dialed, and it reports the daemon's own facts to list_environments.
// Suspend parks the runtime this daemon itself runs in, when it has one.
type LocalEnvironment struct {
	name         string
	capabilities map[string]string
	suspend      func(ctx context.Context) error
}

// NewLocalEnvironment names this daemon, what it reports about itself, and how
// its own runtime is parked, nil when it runs on bare metal.
func NewLocalEnvironment(name string, capabilities map[string]string, suspend func(ctx context.Context) error) *LocalEnvironment {
	return &LocalEnvironment{name: name, capabilities: capabilities, suspend: suspend}
}

func (e *LocalEnvironment) Name() string { return e.name }
func (e *LocalEnvironment) Kind() agentdomain.EnvironmentKind {
	return agentdomain.EnvironmentKindLocal
}

func (e *LocalEnvironment) Ensure(context.Context, agentdomain.Session) (agentdomain.Endpoint, error) {
	return agentdomain.Endpoint{}, errors.New("a session cannot be transferred to the daemon it already runs on")
}

func (e *LocalEnvironment) Probe(context.Context) agentdomain.EnvironmentStatus {
	return agentdomain.EnvironmentStatus{Name: e.name, Kind: agentdomain.EnvironmentKindLocal, Reachable: true, Capabilities: e.capabilities}
}

func (e *LocalEnvironment) Suspend(ctx context.Context, _ agentdomain.Session) error {
	if e.suspend == nil {
		return nil
	}
	return e.suspend(ctx)
}

func (e *LocalEnvironment) Delete(context.Context, agentdomain.Session) error { return nil }
