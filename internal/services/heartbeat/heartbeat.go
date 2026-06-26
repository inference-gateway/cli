// Package heartbeat implements a periodic "wake-up" service that
// spawns the agent on a fixed interval to check for pending work. It
// is hosted by the channels-manager daemon (peer to the scheduler)
// and is disabled by default.
//
// Unlike the scheduler, heartbeat does not route output to a channel.
// Each tick fires `infer agent --heartbeat`, the agent runs to
// completion using a tailored system prompt, and the agent's stdout
// is logged. Whatever externally-visible action the agent takes (e.g.
// posting to Telegram, opening a PR) it does via its own tools.
package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	uuid "github.com/google/uuid"
	logger "github.com/inference-gateway/cli/internal/logger"
	agentrunner "github.com/inference-gateway/cli/internal/services/agentrunner"
)

// ExecCommandFn matches exec.CommandContext. Exposed for tests.
type ExecCommandFn func(ctx context.Context, name string, args ...string) *exec.Cmd

// Config bundles the runtime knobs the heartbeat service needs. It is
// derived from config.HeartbeatConfig at startup time so the service
// stays decoupled from the broader Config type.
type Config struct {
	Interval     time.Duration
	InitialDelay time.Duration
	Model        string
	Prompt       string
}

// Service drives the heartbeat ticker. Single global instance per
// daemon; constructed by NewService and lifecycle-managed via
// Start/Stop.
type Service struct {
	cfg        Config
	execCmd    ExecCommandFn
	binaryPath string

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// running is 1 while a heartbeat agent run is in flight. Used to
	// suppress overlapping ticks if a single run takes longer than the
	// configured interval.
	running atomic.Int32

	started bool
	mu      sync.Mutex
}

// Options bundles dependencies for NewService.
type Options struct {
	Config Config
	// ExecCommand defaults to exec.CommandContext when nil.
	ExecCommand ExecCommandFn
	// BinaryPath defaults to os.Args[0] when empty.
	BinaryPath string
}

// NewService constructs a heartbeat Service. Returns an error if the
// configured interval is non-positive.
func NewService(opts Options) (*Service, error) {
	if opts.Config.Interval <= 0 {
		return nil, errors.New("heartbeat: interval must be > 0")
	}
	execFn := opts.ExecCommand
	if execFn == nil {
		execFn = exec.CommandContext
	}
	bin := opts.BinaryPath
	if bin == "" {
		bin = os.Args[0]
	}
	return &Service{
		cfg:        opts.Config,
		execCmd:    execFn,
		binaryPath: bin,
	}, nil
}

// Start launches the ticker goroutine. The supplied context's
// cancellation is propagated to in-flight agent subprocesses on
// shutdown. Calling Start more than once is a no-op.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.started = true

	logger.Info("heartbeat service started",
		"interval", s.cfg.Interval.String(),
		"initial_delay", s.cfg.InitialDelay.String(),
	)

	s.wg.Add(1)
	go s.run()
	return nil
}

// Stop cancels the ticker and waits for any in-flight heartbeat run
// to terminate. Honours the supplied context as a hard deadline.
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.cancel()
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		logger.Info("heartbeat service stopped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("heartbeat stop: %w", ctx.Err())
	}
}

func (s *Service) run() {
	defer s.wg.Done()

	if s.cfg.InitialDelay > 0 {
		select {
		case <-time.After(s.cfg.InitialDelay):
		case <-s.ctx.Done():
			return
		}
	}

	s.fireGuarded()

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.fireGuarded()
		case <-s.ctx.Done():
			return
		}
	}
}

// fireGuarded suppresses overlapping ticks. If a previous heartbeat
// run is still alive, the current tick is skipped - the next one will
// pick it up.
func (s *Service) fireGuarded() {
	if !s.running.CompareAndSwap(0, 1) {
		logger.Warn("heartbeat tick skipped - previous run still in flight")
		return
	}
	defer s.running.Store(0)

	if err := s.fire(s.ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		logger.Error("heartbeat run failed", "error", err)
	}
}

// fire spawns a single `infer agent --heartbeat` subprocess (via the shared
// agentrunner) and streams its stdout to the logger. Each fire gets a fresh
// UUID session ID so no context carries between ticks.
func (s *Service) fire(ctx context.Context) error {
	sessionID := uuid.New().String()
	logger.Info("heartbeat tick - spawning agent",
		"session_id", sessionID,
		"model", s.cfg.Model,
	)

	res, err := agentrunner.Run(ctx, agentrunner.Options{
		BinaryPath: s.binaryPath,
		Exec:       s.execCmd,
		SessionID:  sessionID,
		Prompt:     s.cfg.Prompt,
		Model:      s.cfg.Model,
		Heartbeat:  true,
		OnLine: func(line []byte) {
			if msg := strings.TrimSpace(string(line)); msg != "" {
				logger.Info("heartbeat agent output", "session_id", sessionID, "line", msg)
			}
		},
	})
	if err != nil {
		if res.Stderr != "" {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(res.Stderr))
		}
		return err
	}
	logger.Info("heartbeat tick complete", "session_id", sessionID)
	return nil
}
