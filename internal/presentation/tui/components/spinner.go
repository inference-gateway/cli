package components

import (
	"time"

	spinner "charm.land/bubbles/v2/spinner"
)

// newModernSpinner returns the app-wide spinner (braille dots, like
// spinner.Dot but without its trailing space so frames are width 1),
// unstyled so callers keep coloring frames themselves.
func newModernSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"},
		FPS:    time.Second / 10,
	}
	return s
}
