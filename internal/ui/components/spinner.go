package components

import (
	"time"

	spinner "charm.land/bubbles/v2/spinner"
)

// newModernSpinner returns the app-wide spinner (◐◓◑◒ at 10 FPS), unstyled so
// callers keep coloring frames themselves.
func newModernSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: []string{"◐", "◓", "◑", "◒"},
		FPS:    100 * time.Millisecond,
	}
	return s
}
