package utils

import (
	"sync"
	"testing"

	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

func TestSubagentTracker_AddSubagent(t *testing.T) {
	tests := []struct {
		name    string
		preAdd  *scheddomain.SubagentState
		add     *scheddomain.SubagentState
		wantErr bool
	}{
		{
			name: "success",
			add:  &scheddomain.SubagentState{ID: "a", Label: "one", Status: scheddomain.SubagentRunning},
		},
		{
			name:    "duplicate ID rejected",
			preAdd:  &scheddomain.SubagentState{ID: "a", Label: "one", Status: scheddomain.SubagentRunning},
			add:     &scheddomain.SubagentState{ID: "a", Label: "one", Status: scheddomain.SubagentRunning},
			wantErr: true,
		},
		{
			name:    "nil state rejected",
			add:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewSubagentTracker()
			if tt.preAdd != nil {
				if err := tr.AddSubagent(tt.preAdd); err != nil {
					t.Fatalf("AddSubagent(preAdd): %v", err)
				}
			}

			err := tr.AddSubagent(tt.add)

			if tt.wantErr && err == nil {
				t.Fatalf("expected AddSubagent to error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("AddSubagent: %v", err)
			}
		})
	}
}

func TestSubagentTracker_GetSubagent(t *testing.T) {
	tests := []struct {
		name      string
		getID     string
		wantNil   bool
		wantLabel string
	}{
		{name: "existing", getID: "a", wantLabel: "one"},
		{name: "missing", getID: "missing", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewSubagentTracker()
			if err := tr.AddSubagent(&scheddomain.SubagentState{ID: "a", Label: "one", Status: scheddomain.SubagentRunning}); err != nil {
				t.Fatalf("AddSubagent: %v", err)
			}

			if all := tr.GetAllSubagents(); len(all) != 1 {
				t.Fatalf("GetAllSubagents len = %d, want 1", len(all))
			}

			got := tr.GetSubagent(tt.getID)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("GetSubagent(%s) = %+v, want nil", tt.getID, got)
				}
				return
			}

			if got == nil || got.Label != tt.wantLabel {
				t.Fatalf("GetSubagent returned %+v", got)
			}
		})
	}
}

func TestSubagentTracker_RemoveSubagent(t *testing.T) {
	tests := []struct {
		name     string
		removeID string
		wantErr  bool
	}{
		{name: "existing", removeID: "a"},
		{name: "missing", removeID: "missing", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewSubagentTracker()
			if err := tr.AddSubagent(&scheddomain.SubagentState{ID: "a", Label: "one", Status: scheddomain.SubagentRunning}); err != nil {
				t.Fatalf("AddSubagent: %v", err)
			}

			err := tr.RemoveSubagent(tt.removeID)

			if tt.wantErr && err == nil {
				t.Fatalf("expected RemoveSubagent of missing id to error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("RemoveSubagent: %v", err)
			}
		})
	}
}

func TestSubagentTracker_CountRunning(t *testing.T) {
	tr := NewSubagentTracker()
	_ = tr.AddSubagent(&scheddomain.SubagentState{ID: "r1", Status: scheddomain.SubagentRunning})
	_ = tr.AddSubagent(&scheddomain.SubagentState{ID: "r2", Status: scheddomain.SubagentRunning})
	_ = tr.AddSubagent(&scheddomain.SubagentState{ID: "done", Status: scheddomain.SubagentCompleted})

	if got := tr.CountRunningSubagents(); got != 2 {
		t.Fatalf("CountRunningSubagents = %d, want 2", got)
	}
}

// TestSubagentTracker_ConcurrentStatusReadWrite races SetSubagentStatus (writer)
// against GetSubagent/GetAllSubagents readers that read Status. It fails under
// `go test -race` if the getters hand out the live entry instead of a copy - the
// same write/read pattern as the engine polling countPendingSubagents
// while a job goroutine marks the subagent completed.
func TestSubagentTracker_ConcurrentStatusReadWrite(t *testing.T) {
	tr := NewSubagentTracker()
	const n = 16
	ids := make([]string, n)
	for i := range ids {
		ids[i] = string(rune('a' + i))
		_ = tr.AddSubagent(&scheddomain.SubagentState{ID: ids[i], Mode: scheddomain.SubagentModeHeadless, Status: scheddomain.SubagentRunning})
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(2)
		go func(id string) {
			defer wg.Done()
			for range 200 {
				_ = tr.SetSubagentStatus(id, scheddomain.SubagentCompleted)
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
