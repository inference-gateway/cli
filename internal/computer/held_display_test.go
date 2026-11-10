package computer

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	computermocks "github.com/inference-gateway/cli/tests/mocks/computer"

	config "github.com/inference-gateway/cli/config"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

type openedDisplays struct {
	controllers []*computermocks.FakeDisplayController
}

// drivenControl is a screen control whose holds open fake displays of 1000x500.
func drivenControl() (*ScreenControl, *openedDisplays) {
	opened := &openedDisplays{}
	control := NewScreenControl(config.DefaultConfig(), nil)
	control.display.open = func() (display.DisplayController, error) {
		controller := &computermocks.FakeDisplayController{}
		controller.GetScreenDimensionsReturns(1000, 500, nil)
		opened.controllers = append(opened.controllers, controller)
		return controller, nil
	}
	return control, opened
}

func drive(t *testing.T, control *ScreenControl, input computerdomain.ScreenInput) {
	t.Helper()
	if err := control.Drive(t.Context(), input); err != nil {
		t.Fatalf("Drive(%+v): %v", input, err)
	}
}

func TestAHoldOpensItsDisplayOnceAndClosesItAtTheHandBack(t *testing.T) {
	control, opened := drivenControl()
	if err := control.TakeOver(); err != nil {
		t.Fatal(err)
	}
	if len(opened.controllers) != 0 {
		t.Fatal("taking over must not open the display before an input needs it")
	}

	drive(t, control, computerdomain.ScreenInput{Kind: computerdomain.ActionMove, X: 10, Y: 10})
	drive(t, control, computerdomain.ScreenInput{Kind: computerdomain.ActionClick, X: 20, Y: 20})
	drive(t, control, computerdomain.ScreenInput{Kind: computerdomain.ActionType, Text: "a"})
	if len(opened.controllers) != 1 {
		t.Fatalf("three inputs opened %d displays, want one", len(opened.controllers))
	}
	held := opened.controllers[0]
	if held.MoveMouseCallCount() != 2 || held.ClickMouseCallCount() != 1 || held.TypeTextCallCount() != 1 {
		t.Fatal("every input of the hold must land on the one open display")
	}
	if held.CloseCallCount() != 0 {
		t.Fatal("the display must stay open during the hold")
	}

	control.HandBack()
	if held.CloseCallCount() != 1 {
		t.Fatalf("the hand-back closed the display %d times, want once", held.CloseCallCount())
	}
}

func TestAHoldLooksAtTheScreenAgainOnceItsDisplayIsStale(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		control, opened := drivenControl()
		if err := control.TakeOver(); err != nil {
			t.Fatal(err)
		}
		move := computerdomain.ScreenInput{Kind: computerdomain.ActionMove, X: 10, Y: 10}

		drive(t, control, move)
		time.Sleep(displayFreshness - time.Millisecond)
		drive(t, control, move)
		if len(opened.controllers) != 1 {
			t.Fatalf("a fresh display was reopened: %d opened", len(opened.controllers))
		}

		time.Sleep(time.Millisecond)
		drive(t, control, move)
		if len(opened.controllers) != 2 {
			t.Fatalf("a stale display was not reopened: %d opened", len(opened.controllers))
		}
		if opened.controllers[0].CloseCallCount() != 1 {
			t.Fatal("the stale display must be closed when it is replaced")
		}
		control.HandBack()
	})
}

func TestTheHandBackReleasesAHeldButtonOnTheOpenDisplay(t *testing.T) {
	control, opened := drivenControl()
	if err := control.TakeOver(); err != nil {
		t.Fatal(err)
	}
	drive(t, control, computerdomain.ScreenInput{Kind: computerdomain.ActionMouseDown, X: 5, Y: 5, Button: "right"})

	control.HandBack()

	if len(opened.controllers) != 1 {
		t.Fatalf("releasing the button opened %d displays, want the one already open", len(opened.controllers))
	}
	held := opened.controllers[0]
	if held.ReleaseMouseCallCount() != 1 {
		t.Fatalf("released %d buttons, want the one still down", held.ReleaseMouseCallCount())
	}
	if _, button := held.ReleaseMouseArgsForCall(0); button != display.MouseButtonRight {
		t.Fatalf("released the %s button, want right", button)
	}
	if held.CloseCallCount() != 1 {
		t.Fatal("the display must close after the release")
	}
}

func TestAnInputAfterTheHandBackOpensNoDisplay(t *testing.T) {
	control, opened := drivenControl()
	if err := control.TakeOver(); err != nil {
		t.Fatal(err)
	}
	control.HandBack()

	err := control.Drive(t.Context(), computerdomain.ScreenInput{Kind: computerdomain.ActionMove, X: 1, Y: 1})
	if !errors.Is(err, computerdomain.ErrScreenNotHeld) {
		t.Fatalf("Drive after the hand-back = %v, want ErrScreenNotHeld", err)
	}
	if len(opened.controllers) != 0 {
		t.Fatal("an input for a screen nobody holds must not open the display")
	}
}

func TestADisplayThatReportsNoSizeIsClosedAndRefused(t *testing.T) {
	control := NewScreenControl(config.DefaultConfig(), nil)
	sizeless := &computermocks.FakeDisplayController{}
	control.display.open = func() (display.DisplayController, error) { return sizeless, nil }
	if err := control.TakeOver(); err != nil {
		t.Fatal(err)
	}

	err := control.Drive(t.Context(), computerdomain.ScreenInput{Kind: computerdomain.ActionMove, X: 1, Y: 1})
	if err == nil {
		t.Fatal("a display without a size cannot be driven")
	}
	if sizeless.CloseCallCount() != 1 {
		t.Fatal("a display that cannot be used must be closed")
	}
}
