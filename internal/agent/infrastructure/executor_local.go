package infrastructure

import (
	"context"
	"errors"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// LocalExecutor is this daemon as an executor: sessions it owns run here, so
// it is never dialed, and it reports the daemon's own facts to list_executors.
// Suspend parks the runtime this daemon itself runs in, when it has one.
type LocalExecutor struct {
	name         string
	capabilities map[string]string
	suspend      func(ctx context.Context) error
}

// NewLocalExecutor names this daemon, what it reports about itself, and how
// its own runtime is parked, nil when it runs on bare metal.
func NewLocalExecutor(name string, capabilities map[string]string, suspend func(ctx context.Context) error) *LocalExecutor {
	return &LocalExecutor{name: name, capabilities: capabilities, suspend: suspend}
}

func (e *LocalExecutor) Name() string                   { return e.name }
func (e *LocalExecutor) Kind() agentdomain.ExecutorKind { return agentdomain.ExecutorKindLocal }

func (e *LocalExecutor) Ensure(context.Context, agentdomain.Session) (agentdomain.Endpoint, error) {
	return agentdomain.Endpoint{}, errors.New("a session cannot be transferred to the daemon it already runs on")
}

func (e *LocalExecutor) Probe(context.Context) agentdomain.ExecutorStatus {
	return agentdomain.ExecutorStatus{Name: e.name, Kind: agentdomain.ExecutorKindLocal, Reachable: true, Capabilities: e.capabilities}
}

func (e *LocalExecutor) Suspend(ctx context.Context, _ agentdomain.Session) error {
	if e.suspend == nil {
		return nil
	}
	return e.suspend(ctx)
}

func (e *LocalExecutor) Delete(context.Context, agentdomain.Session) error { return nil }
