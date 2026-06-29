package services

import (
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestHasPending_ExcludesInteractiveSubagents guards the fix for #678: a running
// interactive subagent is a live, user-driven tmux pane managed by the subagent
// tools, so it must NOT count as pending background work - otherwise a headless
// run that opened one would hang at exit waiting for it to "finish".
func TestHasPending_ExcludesInteractiveSubagents(t *testing.T) {
	reg := NewBackgroundTaskRegistry(4)

	if err := reg.AddSubagent(&domain.SubagentState{
		ID: "i1", Mode: domain.SubagentModeInteractive, Status: domain.SubagentRunning,
	}); err != nil {
		t.Fatalf("AddSubagent: %v", err)
	}
	if reg.HasPending() {
		t.Fatalf("a running interactive subagent must not count as pending")
	}

	if err := reg.AddSubagent(&domain.SubagentState{
		ID: "h1", Mode: domain.SubagentModeHeadless, Status: domain.SubagentRunning,
	}); err != nil {
		t.Fatalf("AddSubagent: %v", err)
	}
	if !reg.HasPending() {
		t.Fatalf("a running headless subagent should count as pending")
	}
}
