package computer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// ScreenControl keeps the take-over invariant this context owns: exactly one
// actor, the person at the keyboard or the agent, drives the screen at a
// time. Takeover claims it for the person, HandBack releases it and wakes the
// executor actions waiting on it, and Drive runs one person input against the
// display. The screen lock is the worker's, either from an action it already
// took or because the person is the authority here, so Drive takes none.
type ScreenControl struct {
	cfg      *config.Config
	notifier agentdomain.UINotifier

	mu       sync.Mutex
	held     bool
	released chan struct{}
}

// NewScreenControl creates the screen control; the notifier may be nil, in
// which case take-overs publish nothing.
func NewScreenControl(cfg *config.Config, notifier agentdomain.UINotifier) *ScreenControl {
	if notifier == nil {
		notifier = agentdomain.NoopUINotifier{}
	}
	return &ScreenControl{cfg: cfg, notifier: notifier}
}

// TakeOver claims the screen for the person. A second claim, from another tab
// of the session or another web viewer, is refused.
func (s *ScreenControl) TakeOver() error {
	s.mu.Lock()
	if s.held {
		s.mu.Unlock()
		return errors.New("the screen is already held by the person")
	}
	s.held = true
	s.released = make(chan struct{})
	s.mu.Unlock()
	s.notifier.Notify(agentdomain.ScreenTakenOverEvent{Timestamp: time.Now()})
	return nil
}

// HandBack releases the screen to the agent and lets go of every action the
// executor parked on Held. Releasing an unheld screen is a no-op.
func (s *ScreenControl) HandBack() {
	s.mu.Lock()
	if !s.held {
		s.mu.Unlock()
		return
	}
	released := s.released
	s.held = false
	s.mu.Unlock()
	close(released)
	s.notifier.Notify(agentdomain.ScreenHandedBackEvent{Timestamp: time.Now()})
}

// Held reports whether the person currently drives the screen.
func (s *ScreenControl) Held() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.held
}

// WaitRelease blocks while the person drives the screen. It re-checks held
// after every release, so a claim taken in between parks again.
func (s *ScreenControl) WaitRelease(ctx context.Context) error {
	for {
		s.mu.Lock()
		if !s.held {
			s.mu.Unlock()
			return nil
		}
		released := s.released
		s.mu.Unlock()
		select {
		case <-released:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Drive performs one person input in the frame space the Screen view sends:
// it scales the frame coordinates to the display the way the Computer tool's
// observations map back, and runs the action without the tool layer - no
// approval, no judge, no conversation entry. The person at the keyboard is
// the authority.
func (s *ScreenControl) Drive(ctx context.Context, input computerdomain.ScreenInput) error {
	action, err := input.Action()
	if err != nil {
		return err
	}
	provider, err := display.DetectDisplay()
	if err != nil {
		return err
	}
	controller, err := provider.GetController()
	if err != nil {
		return err
	}
	defer func() { _ = controller.Close() }()

	screenW, screenH, err := controller.GetScreenDimensions(ctx)
	if err != nil {
		return err
	}
	if screenW <= 0 || screenH <= 0 {
		return fmt.Errorf("no usable display (reported %dx%d)", screenW, screenH)
	}
	frameW, frameH := s.cfg.ComputerUse.Screenshot.FitDims(screenW, screenH)
	obs := &computerdomain.Observation{Width: frameW, Height: frameH}
	return inputDispatch(ctx, controller, action, obs, screenW, screenH, nil)
}
