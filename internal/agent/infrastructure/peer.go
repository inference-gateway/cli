package infrastructure

import (
	"context"
	"fmt"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// The hello and ack fields two daemons exchange: the client kind a peer
// declares, its environment identity, and what the host answers about itself.
const (
	PeerClientKind  = "daemon"
	AttrEnvironment = "environment"
	AttrVersion     = "version"
	AttrStorageID   = "storage_id"
)

// PeerInfo is what a peer daemon declared about itself in its ack.
type PeerInfo struct {
	Environment string
	Version     string
	StorageID   string
}

// PeerDialer dials other daemons as this one, over their binding.
type PeerDialer struct {
	Me        string
	Handshake agui.Handshake
}

// Dial connects to the binding an endpoint reaches and returns the connection
// with what the peer declared about itself.
func (d PeerDialer) Dial(ctx context.Context, endpoint agentdomain.Endpoint, handler agui.Handler) (*agui.Conn, PeerInfo, error) {
	conn, err := agui.Dial(ctx, agui.DialConfig{
		Addr:       endpoint.Addr,
		Token:      endpoint.Token,
		Kind:       PeerClientKind,
		HelloAttrs: map[string]string{AttrEnvironment: d.Me},
		Handshake:  d.Handshake,
		TLS:        endpoint.TLS,
	}, handler)
	if err != nil {
		return nil, PeerInfo{}, fmt.Errorf("reaching the environment's daemon: %w", err)
	}
	return conn, PeerInfo{
		Environment: conn.AckAttr(AttrEnvironment),
		Version:     conn.AckAttr(AttrVersion),
		StorageID:   conn.AckAttr(AttrStorageID),
	}, nil
}
