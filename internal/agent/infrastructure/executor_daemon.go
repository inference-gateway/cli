package infrastructure

import (
	"context"
	"crypto/tls"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// probeTimeout bounds one reachability check of a remote daemon.
const probeTimeout = 5 * time.Second

// DaemonExecutor is a standalone daemon reachable at a fixed address over TLS
// with a shared token. Its sessions live wherever that daemon keeps them, so
// there is nothing to suspend or delete from here.
type DaemonExecutor struct {
	entry config.ExecutorEntry
	tls   *tls.Config
	dial  PeerDialer
}

// NewDaemonExecutor resolves the entry's TLS once and keeps the dialer.
func NewDaemonExecutor(entry config.ExecutorEntry, provider *TLSProvider, dial PeerDialer) (*DaemonExecutor, error) {
	tlsConfig, err := provider.ClientForDaemon(entry)
	if err != nil {
		return nil, err
	}
	return &DaemonExecutor{entry: entry, tls: tlsConfig, dial: dial}, nil
}

func (e *DaemonExecutor) Name() string                   { return e.entry.Name }
func (e *DaemonExecutor) Kind() agentdomain.ExecutorKind { return agentdomain.ExecutorKindDaemon }

func (e *DaemonExecutor) Ensure(context.Context, agentdomain.Session) (agentdomain.Endpoint, error) {
	return agentdomain.Endpoint{Addr: e.entry.Address, TLS: e.tls, Token: e.entry.Token}, nil
}

// Probe dials the daemon and reports what its ack declared.
func (e *DaemonExecutor) Probe(ctx context.Context) agentdomain.ExecutorStatus {
	status := agentdomain.ExecutorStatus{Name: e.entry.Name, Kind: agentdomain.ExecutorKindDaemon}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	endpoint, _ := e.Ensure(ctx, agentdomain.Session{})
	conn, info, err := e.dial.Dial(ctx, endpoint, silentHandler{})
	if err != nil {
		status.Error = err.Error()
		return status
	}
	conn.Close()
	status.Reachable = true
	status.Capabilities = map[string]string{
		AttrExecutor:  info.Executor,
		AttrVersion:   info.Version,
		AttrStorageID: info.StorageID,
		"address":     e.entry.Address,
	}
	if info.Executor != e.entry.Name {
		status.Reachable = false
		status.Error = "the daemon at " + e.entry.Address + " identifies as " + info.Executor
	}
	return status
}

func (e *DaemonExecutor) Suspend(context.Context, agentdomain.Session) error { return nil }
func (e *DaemonExecutor) Delete(context.Context, agentdomain.Session) error  { return nil }

// silentHandler ignores every frame of a probe connection.
type silentHandler struct{}

func (silentHandler) Attach(*agui.Conn)         {}
func (silentHandler) Handle(*agui.Conn, []byte) {}
func (silentHandler) Detach(*agui.Conn)         {}
