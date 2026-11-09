package accessibility

import (
	"context"
	"slices"

	process "github.com/inference-gateway/cli/internal/platform/process"
)

const windowOwnersScript = `global.get_window_actors().map(a => a.meta_window.get_pid())`

// WindowApps finds the processes that show a window on a GNOME Wayland
// computer, minimized ones included.
type WindowApps struct{}

// ShowsWindow reports whether pid, or a process it started, owns a window. A
// shell that cannot be asked answers false, so nothing is taken for a window app.
func (WindowApps) ShowsWindow(ctx context.Context, pid int) bool {
	if !gnomeSession() {
		return false
	}
	var owners []int
	if err := evalShell(ctx, windowOwnersScript, &owners); err != nil {
		return false
	}
	return slices.ContainsFunc(owners, func(owner int) bool {
		return process.StartedBy(ctx, owner, pid)
	})
}
