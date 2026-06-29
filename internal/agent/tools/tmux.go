package tools

import (
	"context"
	"os/exec"
	"strings"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// NewPaneInspector returns a services.PaneInspector (a func yielding a
// domain.PaneObservation) backed by the subagent result file, the approval
// sidecar, and the tmux pane's liveness. It is injected into the SubagentPoller
// in chat mode so it can watch interactive subagents for completion and pending
// approvals. It NEVER returns pane content as the delivered message - the pane is
// rendered TUI chrome (input box, status bar), which is noise in the main
// conversation; only the result file's last assistant message is delivered. The
// Screen snapshot it returns is used ONLY for the poller's idle-by-stability
// check, never delivered.
func NewPaneInspector() func(ctx context.Context, paneID, sessionID string) domain.PaneObservation {
	return func(ctx context.Context, paneID, sessionID string) domain.PaneObservation {
		obs := domain.PaneObservation{Harvested: readSubagentResultMessage(sessionID)}
		if summary, awaiting := readSubagentApproval(sessionID); awaiting {
			obs.AwaitingApproval = true
			obs.ApprovalSummary = summary
		}
		switch tmuxPaneState(ctx, paneID) {
		case paneGone:
			obs.Gone = true
			return obs
		case paneDead:
			obs.Dead = true
			return obs
		}
		obs.Screen = tmuxCapturePaneTail(ctx, paneID, defaultPaneTailLines)
		return obs
	}
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
	paneGone paneState = iota
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

// tmuxCapturePane captures the full visible content of a pane as plain text
// (escape sequences stripped), trailing whitespace trimmed. Unlike
// tmuxCapturePaneTail it preserves the rendered layout (internal blank lines)
// so the caller sees the TUI as drawn - used by ReadSubagentScreen for TUI
// inspection/testing. Returns "" if the pane cannot be read.
func tmuxCapturePane(ctx context.Context, paneID string) string {
	if paneID == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", paneID).Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), " \t\n")
}

// tmuxSendKeys sends literal text and/or named keys to a tmux pane. Literal text
// is sent with -l so shell metacharacters are typed verbatim (never interpreted
// as tmux key names); named keys (Enter, Up, Escape, ...) are sent without -l so
// tmux resolves them. A blank paneID is a no-op. Shared by sendTaskToPane (launch
// task), SendSubagentInput (re-prompt / TUI drive) and ApproveSubagent.
func tmuxSendKeys(ctx context.Context, paneID, text string, keys []string) error {
	if paneID == "" {
		return nil
	}
	if text != "" {
		if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", paneID, "-l", text).Run(); err != nil {
			return err
		}
	}
	for _, k := range keys {
		if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", paneID, k).Run(); err != nil {
			return err
		}
	}
	return nil
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
