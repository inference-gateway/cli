package utils

import (
	"sync"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
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

// TestSubagentTracker_ConcurrentStatusReadWrite races SetSubagentStatus (writer)
// against GetSubagent/GetAllSubagents readers that read Status. It fails under
// `go test -race` if the getters hand out the live entry instead of a copy - the
// same write/read pattern as the BackgroundTasksWaiter polling countPendingSubagents
// while a job goroutine marks the subagent completed.
func TestSubagentTracker_ConcurrentStatusReadWrite(t *testing.T) {
	tr := NewSubagentTracker()
	const n = 16
	ids := make([]string, n)
	for i := range ids {
		ids[i] = string(rune('a' + i))
		_ = tr.AddSubagent(&domain.SubagentState{ID: ids[i], Mode: domain.SubagentModeHeadless, Status: domain.SubagentRunning})
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(2)
		go func(id string) {
			defer wg.Done()
			for range 200 {
				_ = tr.SetSubagentStatus(id, domain.SubagentCompleted)
			}
		}(id)
		go func(id string) {
			defer wg.Done()
			for range 200 {
				if s := tr.GetSubagent(id); s != nil {
					_ = s.Status
				}
				for _, s := range tr.GetAllSubagents() {
					_ = s.Status
				}
			}
		}(id)
	}
	wg.Wait()
}
