package computer

import (
	"context"
	"fmt"
	"sync"
	"time"

	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// displayFreshness is how long a hold drives the display it opened before it
// looks at the screen again.
const displayFreshness = 2 * time.Second

// heldDisplay is the display a person drives during one hold. It opens on the
// first input, serves the inputs that follow one at a time and closes at the
// hand-back. Past displayFreshness it reopens, so a screen resized mid-hold is
// driven at its new size.
type heldDisplay struct {
	open func() (display.DisplayController, error)

	mu         sync.Mutex
	holding    bool
	controller display.DisplayController
	screenW    int
	screenH    int
	openedAt   time.Time
}

func openDisplay() (display.DisplayController, error) {
	provider, err := display.DetectDisplay()
	if err != nil {
		return nil, err
	}
	return provider.GetController()
}

// begin starts a hold. The display opens with the first input, not here, so
// a person can claim a screen whose display is briefly away.
func (h *heldDisplay) begin() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.holding = true
}

// drive runs one input against the open display and its screen size, and
// refuses once the hold has ended.
func (h *heldDisplay) drive(ctx context.Context, input func(controller display.DisplayController, screenW, screenH int) error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.holding {
		return computerdomain.ErrScreenNotHeld
	}
	if err := h.freshen(ctx); err != nil {
		return err
	}
	return input(h.controller, h.screenW, h.screenH)
}

// end finishes the hold: it lets go of the buttons still pressed, so the
// agent does not get the screen back mid-drag, and closes the display.
func (h *heldDisplay) end(stillPressed []string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.holding = false
	if len(stillPressed) > 0 && h.freshen(context.Background()) == nil {
		for _, button := range stillPressed {
			_ = h.controller.ReleaseMouse(context.Background(), display.ParseMouseButton(button))
		}
	}
	h.shut()
}

func (h *heldDisplay) freshen(ctx context.Context) error {
	if h.controller != nil && time.Since(h.openedAt) < displayFreshness {
		return nil
	}
	h.shut()
	controller, err := h.open()
	if err != nil {
		return err
	}
	screenW, screenH, err := controller.GetScreenDimensions(ctx)
	if err == nil && (screenW <= 0 || screenH <= 0) {
		err = fmt.Errorf("no usable display (reported %dx%d)", screenW, screenH)
	}
	if err != nil {
		_ = controller.Close()
		return err
	}
	h.controller, h.screenW, h.screenH, h.openedAt = controller, screenW, screenH, time.Now()
	return nil
}

func (h *heldDisplay) shut() {
	if h.controller == nil {
		return
	}
	_ = h.controller.Close()
	h.controller = nil
}
