//go:build !linux

package accessibility

import (
	"context"
)

// WindowApps finds the processes that show a window on the computer's display.
// Only a GNOME Wayland computer is asked, so here nothing is a window app.
type WindowApps struct{}

// ShowsWindow reports false: this platform has no window apps to tell apart.
func (WindowApps) ShowsWindow(context.Context, int) bool {
	return false
}
