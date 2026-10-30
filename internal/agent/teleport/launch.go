package teleport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// resolveTimeout bounds deciding where a thread runs, provisioning included.
const resolveTimeout = 10 * time.Minute

// LaunchWorker is the seam the registry launches threads through. It answers
// at once with a worker that connects in the background: to a process here
// when this daemon owns the session or nobody does, to the owning environment's
// daemon when another one does. The registry holds its lock while launching,
// so nothing here may block.
func (s *Service) LaunchWorker(key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions) (sessionsdomain.Worker, error) {
	w := newLazyWorker()
	go s.resolve(w, key, opts)
	return w, nil
}

// resolve picks the worker a thread gets and attaches it to the lazy worker.
func (s *Service) resolve(w *lazyWorker, key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions) {
	ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
	defer cancel()
	lease, found, err := s.leases.GetLease(ctx, key.ConversationID)
	if err != nil {
		w.fail(describe(err, "reading the session's lease").Error())
		return
	}
	if !found && opts.Environment != "" && opts.Environment != s.me {
		s.relayNamed(ctx, w, key, opts)
		return
	}
	if !found || lease.Environment == s.me {
		s.launchLocal(ctx, w, key, opts, lease.TeleportID)
		return
	}
	remote, err := s.connectRemote(ctx, key, lease)
	if err == nil {
		w.attach(remote)
		return
	}
	if lease.Live(time.Now()) {
		w.fail(fmt.Sprintf("session %s is owned by environment %s until %s and cannot be reached: %v",
			key.ConversationID, lease.Environment, lease.Until.Format(time.RFC3339), err))
		return
	}
	logger.Warn("teleport reclaims a session whose environment is unreachable and whose lease lapsed",
		"conversation_id", key.ConversationID, "environment", lease.Environment, "error", err)
	s.launchLocal(ctx, w, key, opts, "")
}

// relayNamed relays a thread no lease places to the environment its client
// named, which holds the conversation. Unlike a lapsed lease, an unreachable
// named environment never falls back to a local worker, which would start the
// conversation empty here.
func (s *Service) relayNamed(ctx context.Context, w *lazyWorker, key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions) {
	placed := agentdomain.Lease{ConversationID: key.ConversationID, Environment: opts.Environment, Workspace: opts.Workspace}
	remote, err := s.connectRemote(ctx, key, placed)
	if err != nil {
		w.fail(fmt.Sprintf("session %s cannot be reached on environment %s: %v", key.ConversationID, opts.Environment, err))
		return
	}
	w.attach(remote)
}

// launchLocal claims the session for this daemon and starts a process worker
// with a lease heartbeat.
func (s *Service) launchLocal(ctx context.Context, w *lazyWorker, key sessionsdomain.ThreadKey, opts sessionsdomain.ThreadOptions, teleportID string) {
	lease := agentdomain.Lease{ConversationID: key.ConversationID, Environment: s.me, Workspace: key.ProjectDir, TeleportID: teleportID, Until: time.Now().Add(s.ownedTTL())}
	holder, ok, err := s.leases.ClaimLease(ctx, lease)
	if err != nil {
		w.fail(describe(err, "claiming the session's lease").Error())
		return
	}
	if !ok {
		w.fail(fmt.Sprintf("session %s is owned by environment %s", key.ConversationID, holder.Environment))
		return
	}
	opts.Env = append(opts.Env, s.credentialsFor(key.ConversationID)...)
	inner, err := s.launch(key, opts)
	if err != nil {
		_ = s.leases.ReleaseLease(ctx, key.ConversationID, s.me)
		w.fail(err.Error())
		return
	}
	w.attach(&heartbeatWorker{Worker: inner, stop: s.keepAlive(key.ConversationID, inner)})
}

// keepAlive renews this daemon's lease while the worker runs and stops the
// worker the moment another environment holds the row.
func (s *Service) keepAlive(conversationID string, worker sessionsdomain.Worker) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(leaseHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			err := s.leases.RenewLease(ctx, conversationID, s.me, time.Now().Add(s.ownedTTL()))
			if errors.Is(err, agentdomain.ErrLeaseLost) {
				logger.Warn("teleport stops a worker whose session another environment took over", "conversation_id", conversationID)
				worker.Stop()
				return
			}
			if err != nil {
				logger.Debug("teleport could not renew a session lease", "conversation_id", conversationID, "error", err)
			}
		}
	}()
	return cancel
}

// heartbeatWorker stops the lease heartbeat with the worker.
type heartbeatWorker struct {
	sessionsdomain.Worker
	stop func()
}

func (w *heartbeatWorker) Stop() {
	w.stop()
	w.Worker.Stop()
}

// lazyWorker is a worker whose connection is still being decided. It buffers
// what is sent to it, then replays into the real worker, or ends with one
// RUN_ERROR when no worker could be reached.
type lazyWorker struct {
	mu      sync.Mutex
	inner   sessionsdomain.Worker
	pending [][]byte
	failed  error
	stopped bool
	lines   chan []byte
}

func newLazyWorker() *lazyWorker {
	return &lazyWorker{lines: make(chan []byte, 64)}
}

func (w *lazyWorker) Send(frame []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case w.failed != nil:
		return w.failed
	case w.inner == nil:
		w.pending = append(w.pending, append([]byte(nil), frame...))
		return nil
	default:
		return w.inner.Send(frame)
	}
}

func (w *lazyWorker) Lines() <-chan []byte {
	return w.lines
}

func (w *lazyWorker) Stop() {
	w.mu.Lock()
	inner, stopped := w.inner, w.stopped
	w.stopped = true
	w.mu.Unlock()
	if stopped {
		return
	}
	if inner != nil {
		inner.Stop()
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed == nil {
		w.failed = errors.New("the worker was stopped before it connected")
		close(w.lines)
	}
}

// attach hands the lazy worker its connection, replays the buffered frames
// and relays its lines until it exits.
func (w *lazyWorker) attach(inner sessionsdomain.Worker) {
	w.mu.Lock()
	if w.stopped || w.failed != nil {
		w.mu.Unlock()
		inner.Stop()
		return
	}
	w.inner = inner
	pending := w.pending
	w.pending = nil
	w.mu.Unlock()
	for _, frame := range pending {
		if err := inner.Send(frame); err != nil {
			logger.Warn("teleport could not replay a frame into the connected worker", "error", err)
		}
	}
	go func() {
		for line := range inner.Lines() {
			w.lines <- line
		}
		close(w.lines)
	}()
}

// fail ends the worker before it connected with one RUN_ERROR, so the
// registry tells the thread's clients and forgets the worker.
func (w *lazyWorker) fail(message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed != nil || w.inner != nil {
		return
	}
	w.failed = errors.New(message)
	w.lines <- runError(message)
	close(w.lines)
}
