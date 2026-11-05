package computer

import (
	"context"
	"fmt"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// inputDispatch drives one input action against the display: pointer kinds
// move and click, scroll wheels, type types, key taps a combo. The publish
// hook reports the action just before it runs - the computer-use activity of
// the agent's tool call. A person driving a taken-over screen passes a no-op.
// Coordinates go in, in frame space.
func inputDispatch(ctx context.Context, controller display.DisplayController, a computerdomain.Action, obs *computerdomain.Observation, screenW, screenH int, publish func(kind computerdomain.ActionKind, x, y int)) error {
	switch a.Kind {
	case computerdomain.ActionMove, computerdomain.ActionClick, computerdomain.ActionDoubleClick, computerdomain.ActionTripleClick:
		return dispatchPointer(ctx, controller, a, obs, screenW, screenH, publish)
	case computerdomain.ActionScroll:
		return dispatchScroll(ctx, controller, a, obs)
	case computerdomain.ActionType:
		return dispatchType(ctx, controller, a, obs, publish)
	case computerdomain.ActionKey:
		return dispatchKey(ctx, controller, a, obs, publish)
	default:
		return fmt.Errorf("unknown action %q", a.Kind)
	}
}

// dispatchPointer scales the frame-space target to screen space, moves the
// pointer there and clicks it for the double and triple click kinds.
func dispatchPointer(ctx context.Context, controller display.DisplayController, a computerdomain.Action, obs *computerdomain.Observation, screenW, screenH int, publish func(kind computerdomain.ActionKind, x, y int)) error {
	if a.Target == nil {
		return fmt.Errorf("action %q requires x and y", a.Kind)
	}
	x, y, err := ScaleAPIToScreen(a.Target.X, a.Target.Y, obs.Width, obs.Height, screenW, screenH)
	if err != nil {
		return err
	}
	publish(a.Kind, x, y)
	if err := controller.MoveMouse(ctx, x, y); err != nil {
		return err
	}
	obs.CursorX, obs.CursorY = a.Target.X, a.Target.Y

	if a.Kind == computerdomain.ActionMove {
		obs.Message = fmt.Sprintf("moved cursor to (%d, %d)", a.Target.X, a.Target.Y)
		return nil
	}
	clicks := 1
	switch a.Kind {
	case computerdomain.ActionDoubleClick:
		clicks = 2
	case computerdomain.ActionTripleClick:
		clicks = 3
	}
	button := display.ParseMouseButton(a.Button)
	if err := controller.ClickMouse(ctx, button, clicks); err != nil {
		return err
	}
	obs.Message = fmt.Sprintf("%s at (%d, %d) with the %s button", a.Kind, a.Target.X, a.Target.Y, button)
	return nil
}

// dispatchScroll wheels the screen by the action's clicks, three as the
// default and a negative amount reversing.
func dispatchScroll(ctx context.Context, controller display.DisplayController, a computerdomain.Action, obs *computerdomain.Observation) error {
	amount := a.Amount
	if amount == 0 {
		amount = 3
	}
	direction := a.Direction
	if direction == "" {
		direction = "vertical"
	}
	if err := controller.ScrollMouse(ctx, amount, direction); err != nil {
		return err
	}
	obs.Message = fmt.Sprintf("scrolled %s by %d", direction, amount)
	return nil
}

// dispatchType types the action's text, bounded like the Computer tool's own
// type action.
func dispatchType(ctx context.Context, controller display.DisplayController, a computerdomain.Action, obs *computerdomain.Observation, publish func(kind computerdomain.ActionKind, x, y int)) error {
	if a.Text == "" {
		return fmt.Errorf("type action requires text")
	}
	if len(a.Text) > maxTypeTextLength {
		return fmt.Errorf("text exceeds the %d character limit", maxTypeTextLength)
	}
	publish(a.Kind, 0, 0)
	if err := controller.TypeText(ctx, a.Text, 0); err != nil {
		return err
	}
	obs.Message = fmt.Sprintf("typed %d characters", len(a.Text))
	return nil
}

// dispatchKey sends the action's key combo.
func dispatchKey(ctx context.Context, controller display.DisplayController, a computerdomain.Action, obs *computerdomain.Observation, publish func(kind computerdomain.ActionKind, x, y int)) error {
	if a.Combo == "" {
		return fmt.Errorf("key action requires combo")
	}
	publish(a.Kind, 0, 0)
	if err := controller.SendKeyCombo(ctx, a.Combo); err != nil {
		return err
	}
	obs.Message = "pressed " + a.Combo
	return nil
}
