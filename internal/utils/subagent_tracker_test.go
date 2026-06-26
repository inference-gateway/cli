package utils

import (
	"testing"

	"github.com/inference-gateway/cli/internal/domain"
)

func TestSubagentTracker_AddGetRemove(t *testing.T) {
	tr := NewSubagentTracker()

	state := &domain.SubagentState{ID: "a", Label: "one", Status: domain.SubagentRunning}
	if err := tr.AddSubagent(state); err != nil {
		t.Fatalf("AddSubagent: %v", err)
	}
	if err := tr.AddSubagent(state); err == nil {
		t.Fatalf("expected duplicate AddSubagent to error")
	}

	if got := tr.GetSubagent("a"); got == nil || got.Label != "one" {
		t.Fatalf("GetSubagent returned %+v", got)
	}
	if got := tr.GetSubagent("missing"); got != nil {
		t.Fatalf("GetSubagent(missing) = %+v, want nil", got)
	}

	if all := tr.GetAllSubagents(); len(all) != 1 {
		t.Fatalf("GetAllSubagents len = %d, want 1", len(all))
	}

	if err := tr.RemoveSubagent("a"); err != nil {
		t.Fatalf("RemoveSubagent: %v", err)
	}
	if err := tr.RemoveSubagent("a"); err == nil {
		t.Fatalf("expected RemoveSubagent of missing id to error")
	}
}

func TestSubagentTracker_CountRunning(t *testing.T) {
	tr := NewSubagentTracker()
	_ = tr.AddSubagent(&domain.SubagentState{ID: "r1", Status: domain.SubagentRunning})
	_ = tr.AddSubagent(&domain.SubagentState{ID: "r2", Status: domain.SubagentRunning})
	_ = tr.AddSubagent(&domain.SubagentState{ID: "done", Status: domain.SubagentCompleted})

	if got := tr.CountRunningSubagents(); got != 2 {
		t.Fatalf("CountRunningSubagents = %d, want 2", got)
	}
}

func TestSubagentTracker_NilState(t *testing.T) {
	tr := NewSubagentTracker()
	if err := tr.AddSubagent(nil); err == nil {
		t.Fatalf("expected AddSubagent(nil) to error")
	}
}
