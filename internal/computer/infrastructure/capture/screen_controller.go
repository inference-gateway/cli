//go:build darwin || windows

package capture

import (
	"context"
	"fmt"

	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// controllerSize returns the logical primary screen size from the display
// controller the Computer tool uses, so recordings share its coordinates.
func controllerSize(ctx context.Context) (int, int, error) {
	provider, err := display.DetectDisplay()
	if err != nil {
		return 0, 0, err
	}
	controller, err := provider.GetController()
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = controller.Close() }()
	w, h, err := controller.GetScreenDimensions(ctx)
	if err != nil {
		return 0, 0, err
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("no usable display (reported %dx%d)", w, h)
	}
	return w, h, nil
}
