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
// time. HandBack wakes the executor actions waiting on the hold. Drive takes
// no screen lock - the person is the authority on the worker's screen.
type ScreenControl struct {
	cfg      *config.Config
	notifier agentdomain.UINotifier

	mu       sync.Mutex
	held     bool
	released chan struct{}
	follower computerdomain.ScreenFollower
	pressed  computerdomain.HeldButtons
}

// unfollowed is the follower of a screen nobody streams.
type unfollowed struct{}

func (unfollowed) HoldChanged(bool) {}

func (unfollowed) InputLanded() {}

// NewScreenControl creates the screen control. The notifier may be nil, in
// which case take-overs publish nothing.
func NewScreenControl(cfg *config.Config, notifier agentdomain.UINotifier) *ScreenControl {
	if notifier == nil {
		notifier = agentdomain.NoopUINotifier{}
	}
	return &ScreenControl{cfg: cfg, notifier: notifier, follower: unfollowed{}}
}

// Follow names who keeps the streamed screen current during a hold. A hold
// that already started is announced at once, so a late follower catches up.
func (s *ScreenControl) Follow(follower computerdomain.ScreenFollower) {
	s.mu.Lock()
	s.follower = follower
	held := s.held
	s.mu.Unlock()
	follower.HoldChanged(held)
}

func (s *ScreenControl) currentFollower() computerdomain.ScreenFollower {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.follower
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
	s.pressed = computerdomain.HeldButtons{}
	follower := s.follower
	s.mu.Unlock()
	follower.HoldChanged(true)
	s.notifier.Notify(agentdomain.ScreenTakenOverEvent{Timestamp: time.Now()})
	return nil
}

// HandBack releases the screen to the agent, lets go of every action the
// executor parked on Held and reports whether the screen was held.
func (s *ScreenControl) HandBack() bool {
	s.mu.Lock()
	if !s.held {
		s.mu.Unlock()
		return false
	}
	released := s.released
	stillPressed := s.pressed.Names()
	s.held = false
	s.pressed = nil
	follower := s.follower
	s.mu.Unlock()
	releaseButtons(stillPressed)
	close(released)
	follower.HoldChanged(false)
	s.notifier.Notify(agentdomain.ScreenHandedBackEvent{Timestamp: time.Now()})
	return true
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

// Drive performs one person input on the held screen, in the frame space the
// Screen view sends. It runs without the tool layer - no approval, no judge,
// no conversation entry - because the person at the keyboard is the authority.
func (s *ScreenControl) Drive(ctx context.Context, input computerdomain.ScreenInput) error {
	if !s.Held() {
		return computerdomain.ErrScreenNotHeld
	}
	action, err := input.Action()
	if err != nil {
		return err
	}
	controller, err := openDisplay()
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
	if err := inputDispatch(ctx, controller, action, obs, screenW, screenH, func(computerdomain.ActionKind, int, int) {}); err != nil {
		return err
	}
	s.track(action)
	s.currentFollower().InputLanded()
	return nil
}

// track remembers the buttons the person holds down. An input that lands as
// the screen is handed back has nothing left to track.
func (s *ScreenControl) track(action computerdomain.Action) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pressed != nil {
		s.pressed.Track(action)
	}
}

func openDisplay() (display.DisplayController, error) {
	provider, err := display.DetectDisplay()
	if err != nil {
		return nil, err
	}
	return provider.GetController()
}

// releaseButtons lets go of the buttons a hold ended with, so the agent does
// not get the screen back mid-drag. A display that is gone has nothing held.
func releaseButtons(buttons []string) {
	if len(buttons) == 0 {
		return
	}
	controller, err := openDisplay()
	if err != nil {
		return
	}
	defer func() { _ = controller.Close() }()
	for _, button := range buttons {
		_ = controller.ReleaseMouse(context.Background(), display.ParseMouseButton(button))
	}
}
