package components

import (
	"time"

	spinner "charm.land/bubbles/v2/spinner"
)

// StreamFrameInterval is how often a streaming reply is repainted: 15fps reads
// as live and halves the viewport rebuilds of a 30fps tick.
const StreamFrameInterval = time.Second / 15

// SpinnerFrameInterval is how long one spinner frame stays on screen. It is a
// whole number of stream frames, so the spinner turns evenly on either cadence.
const SpinnerFrameInterval = 2 * StreamFrameInterval

// spinnerFrames are the app-wide braille dots, like spinner.Dot but without its
// trailing space so frames are width 1.
var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// newModernSpinner returns the app-wide spinner for the views that animate
// their own loading state, unstyled so callers keep coloring frames themselves.
func newModernSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Spinner{Frames: spinnerFrames, FPS: SpinnerFrameInterval}
	return s
}

// spinnerFrame is the glyph shown at now when each frame stays up for step.
// Deriving it from the wall clock keeps every live spinner in step on the
// chat's one live clock, with no ticker of its own. Truncating puts the frame
// changes on the grid the clock ticks on, so no tick lands astride one.
func spinnerFrame(now time.Time, step time.Duration) string {
	return spinnerFrames[int(now.Truncate(step).UnixNano()/int64(step))%len(spinnerFrames)]
}

// formatDuration renders an elapsed time in whole seconds (3s, 1m5s), so live
// counters change once a second and never force a sub-second repaint.
func formatDuration(d time.Duration) string {
	return d.Truncate(time.Second).String()
}
