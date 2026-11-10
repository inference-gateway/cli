package computer

import (
	"testing"

	computermocks "github.com/inference-gateway/cli/tests/mocks/computer"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

func dispatchOnAFullSizeFrame(t *testing.T, controller *computermocks.FakeDisplayController, a computerdomain.Action) {
	t.Helper()
	obs := &computerdomain.Observation{Width: 1000, Height: 500}
	unpublished := func(computerdomain.ActionKind, int, int) {}
	if err := inputDispatch(t.Context(), controller, a, obs, 1000, 500, unpublished); err != nil {
		t.Fatal(err)
	}
}

func TestAButtonPressMovesThePointerThereAndHoldsTheButton(t *testing.T) {
	controller := &computermocks.FakeDisplayController{}
	press := computerdomain.Action{Kind: computerdomain.ActionMouseDown, Target: &computerdomain.Target{X: 40, Y: 30}, Button: "right"}

	dispatchOnAFullSizeFrame(t, controller, press)

	if _, x, y := controller.MoveMouseArgsForCall(0); x != 40 || y != 30 {
		t.Fatalf("moved to (%d, %d), want (40, 30)", x, y)
	}
	if _, button := controller.PressMouseArgsForCall(0); button != display.MouseButtonRight {
		t.Fatalf("pressed the %s button, want right", button)
	}
	if controller.ReleaseMouseCallCount() != 0 || controller.ClickMouseCallCount() != 0 {
		t.Fatal("a press must neither release nor click")
	}
}

func TestAButtonReleaseMovesThePointerThereAndLetsGo(t *testing.T) {
	controller := &computermocks.FakeDisplayController{}
	release := computerdomain.Action{Kind: computerdomain.ActionMouseUp, Target: &computerdomain.Target{X: 90, Y: 60}}

	dispatchOnAFullSizeFrame(t, controller, release)

	if _, x, y := controller.MoveMouseArgsForCall(0); x != 90 || y != 60 {
		t.Fatalf("moved to (%d, %d), want (90, 60)", x, y)
	}
	if _, button := controller.ReleaseMouseArgsForCall(0); button != display.MouseButtonLeft {
		t.Fatalf("released the %s button, want left as the default", button)
	}
	if controller.PressMouseCallCount() != 0 || controller.ClickMouseCallCount() != 0 {
		t.Fatal("a release must neither press nor click")
	}
}
