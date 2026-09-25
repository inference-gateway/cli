//go:build darwin

package capture

import (
	"context"
	"errors"
	"fmt"

	purego "github.com/ebitengine/purego"

	accessibility "github.com/inference-gateway/cli/internal/computer/infrastructure/accessibility"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// PrimaryScreen returns the main display. avfoundation captures it at its
// backing (Retina) resolution, which the recorder crops relative to the
// input size, so the native size is left equal to the logical one.
func PrimaryScreen(ctx context.Context) (Screen, error) {
	w, h, err := controllerSize(ctx)
	return Screen{Width: w, Height: h, NativeWidth: w, NativeHeight: h}, err
}

// Preflight fails when the terminal lacks the Screen Recording permission;
// without it avfoundation silently records only the wallpaper.
func Preflight() error {
	cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil
	}
	symbol, err := purego.Dlsym(cg, "CGPreflightScreenCaptureAccess")
	if err != nil {
		return nil
	}
	var preflight func() bool
	purego.RegisterFunc(&preflight, symbol)
	if !preflight() {
		return errors.New("screen recording permission is not granted: enable your terminal app in System Settings > Privacy & Security > Screen & System Audio Recording, then restart it")
	}
	return nil
}

// WindowBounds returns the focused window of the target application.
func WindowBounds(ctx context.Context, window string) (display.Region, error) {
	if _, err := parseTarget(window); err != nil {
		return display.Region{}, err
	}
	b, err := accessibility.WindowBounds(ctx, window)
	switch {
	case errors.Is(err, accessibility.ErrPermission):
		return display.Region{}, errors.New("window mode needs the Accessibility permission: enable your terminal app in System Settings > Privacy & Security > Accessibility")
	case errors.Is(err, accessibility.ErrElementNotFound):
		return display.Region{}, errNoWindow(window)
	case err != nil:
		return display.Region{}, fmt.Errorf("find window %q: %w", window, err)
	}
	return display.Region{X: b[0], Y: b[1], Width: b[2] - b[0], Height: b[3] - b[1]}, nil
}
