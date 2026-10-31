package teleport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// daemonFrames are the frames a daemon sends every client, not a thread's
// worker lines, so a remote worker drops them.
var daemonFrames = map[string]bool{"thread_activity": true, "browser_extension_status": true}

// peerFrames collects the frames a dialed daemon sends.
type peerFrames struct {
	frames chan []byte
	once   sync.Once
}

func newPeerFrames() *peerFrames {
	return &peerFrames{frames: make(chan []byte, 256)}
}

func (p *peerFrames) Attach(*agui.Conn) {}

func (p *peerFrames) Handle(_ *agui.Conn, frame []byte) {
	select {
	case p.frames <- append([]byte(nil), frame...):
	default:
		logger.Warn("teleport dropped a peer frame the relay could not keep up with")
	}
}

func (p *peerFrames) Detach(*agui.Conn) {
	p.once.Do(func() { close(p.frames) })
}

// await returns the next frame of one of the types, a RUN_ERROR as the error
// it carries, or ctx's error.
func (p *peerFrames) await(ctx context.Context, types ...string) ([]byte, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case frame, ok := <-p.frames:
			if !ok {
				return nil, errors.New("the environment's daemon hung up")
			}
			typ := frameType(frame)
			for _, want := range types {
				if typ == want {
					return frame, nil
				}
			}
			if typ == frameRunError {
				var head struct {
					Message string `json:"message"`
				}
				_ = json.Unmarshal(frame, &head)
				return nil, errors.New(head.Message)
			}
		}
	}
}

// remoteWorker relays a thread's frames to the daemon of the environment that
// owns the session, as if that daemon were the worker.
type remoteWorker struct {
	conn      *agui.Conn
	endpoint  agentdomain.Endpoint
	workspace string
	lines     chan []byte
	cancel    context.CancelFunc
	once      sync.Once
}

// connectRemote reaches the environment named by a lease and relays the thread
// to it, refreshing this daemon's row for that owner while connected.
func (s *Service) connectRemote(ctx context.Context, key sessionsdomain.ThreadKey, lease agentdomain.Lease) (sessionsdomain.Worker, error) {
	environment, ok := s.environments[lease.Environment]
	if !ok {
		return nil, fmt.Errorf("environment %s is not registered in daemon.yaml", lease.Environment)
	}
	endpoint, err := environment.Reach(ctx, s.session(key))
	if err != nil {
		return nil, err
	}
	frames := newPeerFrames()
	conn, info, err := s.dial.Dial(ctx, endpoint, frames)
	if err != nil {
		endpoint.Release()
		return nil, err
	}
	if info.Environment != lease.Environment {
		conn.Close()
		endpoint.Release()
		return nil, fmt.Errorf("the daemon reached for environment %s identifies as %q", lease.Environment, info.Environment)
	}
	refreshCtx, cancel := context.WithCancel(context.Background())
	w := &remoteWorker{conn: conn, endpoint: endpoint, workspace: lease.Workspace, lines: make(chan []byte, 64), cancel: cancel}
	go w.relay(frames)
	go s.refreshRemote(refreshCtx, key.ConversationID, lease.Environment, w)
	return w, nil
}

// refreshRemote keeps this daemon's row for the remote owner live while the
// relay is connected, and drops the relay when the row changed hands.
func (s *Service) refreshRemote(ctx context.Context, conversationID, environment string, w *remoteWorker) {
	ttl := s.entries[environment].EffectiveLeaseTTL()
	ticker := time.NewTicker(leaseHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		err := s.leases.RenewLease(ctx, conversationID, environment, time.Now().Add(ttl))
		if errors.Is(err, agentdomain.ErrLeaseLost) {
			logger.Info("teleport drops a relay whose session changed hands", "conversation_id", conversationID, "environment", environment)
			w.Stop()
			return
		}
	}
}

// relay forwards the peer's frames as worker lines until it hangs up.
func (w *remoteWorker) relay(frames *peerFrames) {
	for frame := range frames.frames {
		if daemonFrames[frameType(frame)] {
			continue
		}
		w.lines <- frame
	}
	close(w.lines)
	w.Stop()
}

// Send forwards one client frame, naming the remote workspace where the frame
// named the client's project dir.
func (w *remoteWorker) Send(frame []byte) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, frame); err != nil {
		return err
	}
	frame = compact.Bytes()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(frame, &fields); err != nil {
		return err
	}
	if _, named := fields["project_dir"]; named {
		fields["project_dir"] = encode(w.workspace)
		frame = encode(fields)
	}
	return w.conn.Send(frame)
}

func (w *remoteWorker) Lines() <-chan []byte {
	return w.lines
}

// Stop closes the relay and releases the endpoint. The remote daemon keeps
// its worker, which its own idle rules stop.
func (w *remoteWorker) Stop() {
	w.once.Do(func() {
		w.cancel()
		w.conn.Close()
		w.endpoint.Release()
	})
}
