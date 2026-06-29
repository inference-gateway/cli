package tools

import (
	"context"
	"os/exec"
	"strings"
)

// chatIdleMarker is the input placeholder shown by the chat TUI when it is
// idle and waiting for input (see internal/ui/components/input_view.go). Its
// presence near the bottom of a subagent's pane means the chat finished its
// turn.
const chatIdleMarker = "Type your message"

// NewPaneInspector returns a services.PaneInspector (a
// func(ctx, paneID) (content string, idle bool, gone bool)) backed by the tmux
// helpers. It is injected into the SubagentPoller in chat mode so it can watch
// interactive subagents for completion. idle is true when the chat input prompt
// has reappeared in the last few lines (the subagent finished its turn) or the
// pane's process exited; gone is true when the pane no longer exists.
func NewPaneInspector() func(ctx context.Context, paneID string) (string, bool, bool) {
	return func(ctx context.Context, paneID string) (string, bool, bool) {
		switch tmuxPaneState(ctx, paneID) {
		case paneGone:
			return "", false, true
		case paneDead:
			// Process exited (kept by remain-on-exit) - definitively done.
			return tmuxCapturePaneTail(ctx, paneID, defaultPaneTailLines), true, false
		}
		content := tmuxCapturePaneTail(ctx, paneID, defaultPaneTailLines)
		return content, lastLinesContain(content, chatIdleMarker, 6), false
	}
}

// lastLinesContain reports whether the marker appears within the last n lines of
// s. Restricting to the tail avoids mistaking the marker appearing inside a
// subagent's response for the idle input prompt at the bottom of the pane.
func lastLinesContain(s, marker string, n int) bool {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Contains(strings.Join(lines, "\n"), marker)
}

// Tail bounds for harvesting a subagent pane's output: enough to show the last
// message or two, never the full scrollback.
const (
	defaultPaneTailLines = 30
	maxPaneTailLines     = 200
)

// paneState describes the liveness of the tmux pane backing an interactive
// subagent.
type paneState int

const (
	paneGone  paneState = iota
	paneAlive
	paneDead
)

// status maps a pane state to the user-facing subagent status word.
func (s paneState) status() string {
	switch s {
	case paneAlive:
		return "running"
	case paneDead:
		return "finished"
	default:
		return "closed"
	}
}

// tmuxPaneState reports whether the given tmux pane is alive, dead (its process
// exited but the pane is kept open by remain-on-exit), or gone (no such pane).
func tmuxPaneState(ctx context.Context, paneID string) paneState {
	if paneID == "" {
		return paneGone
	}
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", paneID, "-F", "#{pane_dead}").Output()
	if err != nil {
		return paneGone
	}
	if strings.TrimSpace(string(out)) == "1" {
		return paneDead
	}
	return paneAlive
}

// tmuxCapturePaneTail captures a tmux pane and returns at most the last maxLines
// non-blank lines - a bounded tail of the subagent's most recent output, never
// the full scrollback. Returns "" if the pane cannot be read.
func tmuxCapturePaneTail(ctx context.Context, paneID string, maxLines int) string {
	if paneID == "" {
		return ""
	}
	if maxLines <= 0 {
		maxLines = defaultPaneTailLines
	}
	if maxLines > maxPaneTailLines {
		maxLines = maxPaneTailLines
	}
	out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", paneID).Output()
	if err != nil {
		return ""
	}
	return tailNonBlankLines(string(out), maxLines)
}

// tmuxKillPane closes a tmux pane. A blank id is a no-op.
func tmuxKillPane(ctx context.Context, paneID string) error {
	if paneID == "" {
		return nil
	}
	return exec.CommandContext(ctx, "tmux", "kill-pane", "-t", paneID).Run()
}

// tailNonBlankLines trims leading/trailing blank lines and returns at most the
// last maxLines lines of s.
func tailNonBlankLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	start := 0
	for start < end && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	lines = lines[start:end]
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}
