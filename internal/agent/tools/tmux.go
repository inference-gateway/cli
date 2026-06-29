package tools

import (
	"context"
	"os/exec"
	"strings"
)

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
