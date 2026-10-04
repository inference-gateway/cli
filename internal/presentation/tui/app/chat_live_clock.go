package app

import (
	"time"

	tea "charm.land/bubbletea/v2"

	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
)

// liveClock is the chat's one repaint clock, replacing a ticker per live
// indicator. It keeps a single tick in flight while the screen needs one and
// lets the chain die when nothing live is on screen, so an idle TUI never wakes.
type liveClock struct {
	epoch    int
	interval time.Duration
}

// liveClockTickMsg is one fired tick, stamped with the chain that armed it.
type liveClockTickMsg struct{ epoch int }

// sync re-arms the clock when the cadence the screen needs changed and returns
// the command scheduling the next tick, nil while the chain in flight fits.
// Arming a new chain bumps the epoch so the superseded tick is dropped.
func (c *liveClock) sync(need time.Duration) tea.Cmd {
	if need == c.interval {
		return nil
	}
	c.epoch++
	c.interval = need
	if need == 0 {
		return nil
	}
	epoch := c.epoch
	return tea.Every(need, func(time.Time) tea.Msg { return liveClockTickMsg{epoch: epoch} })
}

// fire consumes a tick and reports whether it belongs to the chain in flight.
// The chain is spent either way until the next sync re-arms it.
func (c *liveClock) fire(msg liveClockTickMsg) bool {
	if msg.epoch != c.epoch {
		return false
	}
	c.interval = 0
	return true
}

// liveCadence is how often the chat needs a repaint: every stream frame while
// a reply streams, every spinner frame while the status line animates, every
// second while only counters run and never while nothing is live. A prompt
// waiting on the user freezes the turn's timers, so only job rows tick then.
func (app *ChatApplication) liveCadence() time.Duration {
	if app.conversationView != nil && app.conversationView.IsStreaming() {
		return components.StreamFrameInterval
	}
	if !components.AwaitingUserDecision(app.stateManager) {
		if app.statusView != nil && app.statusView.IsShowingSpinner() {
			return components.SpinnerFrameInterval
		}
		if app.toolCallRenderer != nil && app.toolCallRenderer.HasActivePreviews() {
			return time.Second
		}
	}
	if app.subagentList != nil && app.subagentList.HasRows() {
		return time.Second
	}
	return 0
}
