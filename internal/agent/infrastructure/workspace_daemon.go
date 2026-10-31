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

// DaemonWorkspace is a standalone daemon reachable at a fixed address over TLS
// with a shared token. Its sessions live wherever that daemon keeps them, so
// there is nothing to suspend or delete from here.
type DaemonWorkspace struct {
	entry config.WorkspaceEntry
	tls   *tls.Config
	dial  PeerDialer
}

// NewDaemonWorkspace resolves the entry's TLS once and keeps the dialer.
func NewDaemonWorkspace(entry config.WorkspaceEntry, provider *TLSProvider, dial PeerDialer) (*DaemonWorkspace, error) {
	tlsConfig, err := provider.ClientForDaemon(entry)
	if err != nil {
		return nil, err
	}
	return &DaemonWorkspace{entry: entry, tls: tlsConfig, dial: dial}, nil
}

func (e *DaemonWorkspace) Name() string { return e.entry.Name }
func (e *DaemonWorkspace) Kind() agentdomain.WorkspaceKind {
	return agentdomain.WorkspaceKindDaemon
}

func (e *DaemonWorkspace) Ensure(context.Context, agentdomain.Session) (agentdomain.Endpoint, error) {
	return agentdomain.Endpoint{Addr: e.entry.Address, TLS: e.tls, Token: e.entry.Token}, nil
}

func (e *DaemonWorkspace) Reach(ctx context.Context, session agentdomain.Session) (agentdomain.Endpoint, error) {
	return e.Ensure(ctx, session)
}

// Probe dials the daemon and reports what its ack declared.
func (e *DaemonWorkspace) Probe(ctx context.Context) agentdomain.WorkspaceStatus {
	status := agentdomain.WorkspaceStatus{Name: e.entry.Name, Kind: agentdomain.WorkspaceKindDaemon}
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
		AttrWorkspace: info.Workspace,
		AttrVersion:   info.Version,
		AttrStorageID: info.StorageID,
		"address":     e.entry.Address,
	}
	if info.Workspace != e.entry.Name {
		status.Reachable = false
		status.Error = "the daemon at " + e.entry.Address + " identifies as " + info.Workspace
	}
	return status
}

func (e *DaemonWorkspace) Suspend(context.Context, agentdomain.Session) error { return nil }
func (e *DaemonWorkspace) Delete(context.Context, agentdomain.Session) error  { return nil }

// silentHandler ignores every frame of a probe connection.
type silentHandler struct{}

func (silentHandler) Attach(*agui.Conn)         {}
func (silentHandler) Handle(*agui.Conn, []byte) {}
func (silentHandler) Detach(*agui.Conn)         {}
