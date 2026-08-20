package utils

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func createTestShell(id string, state domain.ShellState) *domain.BackgroundShell {
	ctx, cancel := context.WithCancel(context.Background())

	return &domain.BackgroundShell{
		ShellID:      id,
		Command:      "echo test",
		Cmd:          exec.CommandContext(ctx, "echo", "test"),
		StartedAt:    time.Now(),
		State:        state,
		OutputBuffer: NewOutputRingBuffer(1024),
		CancelFunc:   cancel,
		ReadOffset:   0,
	}
}

func mustAddShell(t *testing.T, tracker *shellTracker, shell *domain.BackgroundShell) {
	t.Helper()
	if err := tracker.Add(shell); err != nil {
		t.Fatalf("Add(%s) failed: %v", shell.ShellID, err)
	}
}

func TestNewShellTracker(t *testing.T) {
	tracker := NewShellTracker(5)

	if tracker == nil {
		t.Fatal("NewShellTracker returned nil")
	}

	if tracker.Count() != 0 {
		t.Errorf("Expected count=0, got %d", tracker.Count())
	}

	if tracker.CountRunning() != 0 {
		t.Errorf("Expected countRunning=0, got %d", tracker.CountRunning())
	}
}

func TestShellTracker_Add(t *testing.T) {
	tests := []struct {
		name             string
		maxConcurrent    int
		preAdd           []*domain.BackgroundShell
		add              *domain.BackgroundShell
		wantErr          bool
		wantCount        int
		wantCountRunning int
	}{
		{
			name:             "success",
			maxConcurrent:    5,
			add:              createTestShell("shell-123", domain.ShellStateRunning),
			wantErr:          false,
			wantCount:        1,
			wantCountRunning: 1,
		},
		{
			name:          "duplicate ID rejected",
			maxConcurrent: 5,
			preAdd: []*domain.BackgroundShell{
				createTestShell("shell-123", domain.ShellStateRunning),
			},
			add:              createTestShell("shell-123", domain.ShellStateRunning),
			wantErr:          true,
			wantCount:        1,
			wantCountRunning: 1,
		},
		{
			name:          "max concurrent limit rejects running shell",
			maxConcurrent: 3,
			preAdd: []*domain.BackgroundShell{
				createTestShell("shell-0", domain.ShellStateRunning),
				createTestShell("shell-1", domain.ShellStateRunning),
				createTestShell("shell-2", domain.ShellStateRunning),
			},
			add:              createTestShell("shell-4", domain.ShellStateRunning),
			wantErr:          true,
			wantCount:        3,
			wantCountRunning: 3,
		},
		{
			name:          "max concurrent limit allows completed shell",
			maxConcurrent: 2,
			preAdd: []*domain.BackgroundShell{
				createTestShell("shell-running-0", domain.ShellStateRunning),
				createTestShell("shell-running-1", domain.ShellStateRunning),
			},
			add:              createTestShell("shell-completed", domain.ShellStateCompleted),
			wantErr:          false,
			wantCount:        3,
			wantCountRunning: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewShellTracker(tt.maxConcurrent)
			for _, shell := range tt.preAdd {
				mustAddShell(t, tracker, shell)
			}

			err := tracker.Add(tt.add)

			if tt.wantErr && err == nil {
				t.Fatal("Expected error from Add, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Add failed: %v", err)
			}

			if tracker.Count() != tt.wantCount {
				t.Errorf("Expected count=%d, got %d", tt.wantCount, tracker.Count())
			}

			if tracker.CountRunning() != tt.wantCountRunning {
				t.Errorf("Expected countRunning=%d, got %d", tt.wantCountRunning, tracker.CountRunning())
			}
		})
	}
}

func TestShellTracker_Get(t *testing.T) {
	tests := []struct {
		name    string
		preAdd  []*domain.BackgroundShell
		getID   string
		wantNil bool
		wantID  string
	}{
		{
			name:   "found",
			preAdd: []*domain.BackgroundShell{createTestShell("shell-123", domain.ShellStateRunning)},
			getID:  "shell-123",
			wantID: "shell-123",
		},
		{
			name:    "not found",
			getID:   "nonexistent",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewShellTracker(5)
			for _, shell := range tt.preAdd {
				mustAddShell(t, tracker, shell)
			}

			retrieved := tracker.Get(tt.getID)

			if tt.wantNil {
				if retrieved != nil {
					t.Error("Expected Get to return nil for nonexistent shell")
				}
				return
			}

			if retrieved == nil {
				t.Fatal("Get returned nil")
			}

			if retrieved.ShellID != tt.wantID {
				t.Errorf("Expected ShellID=%s, got %s", tt.wantID, retrieved.ShellID)
			}
		})
	}
}

func TestShellTracker_GetAll(t *testing.T) {
	tests := []struct {
		name    string
		numAdd  int
		wantLen int
	}{
		{name: "three shells", numAdd: 3, wantLen: 3},
		{name: "empty", numAdd: 0, wantLen: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewShellTracker(5)
			for i := 0; i < tt.numAdd; i++ {
				mustAddShell(t, tracker, createTestShell(fmt.Sprintf("shell-%d", i), domain.ShellStateRunning))
			}

			all := tracker.GetAll()

			if len(all) != tt.wantLen {
				t.Errorf("Expected %d shells, got %d", tt.wantLen, len(all))
			}
		})
	}
}

func TestShellTracker_Remove(t *testing.T) {
	tests := []struct {
		name     string
		preAdd   []*domain.BackgroundShell
		removeID string
		wantErr  bool
	}{
		{
			name:     "success",
			preAdd:   []*domain.BackgroundShell{createTestShell("shell-123", domain.ShellStateRunning)},
			removeID: "shell-123",
		},
		{
			name:     "not found",
			removeID: "nonexistent",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewShellTracker(5)
			for _, shell := range tt.preAdd {
				mustAddShell(t, tracker, shell)
			}

			err := tracker.Remove(tt.removeID)

			if tt.wantErr {
				if err == nil {
					t.Fatal("Expected error when removing nonexistent shell, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Remove failed: %v", err)
			}

			if tracker.Count() != 0 {
				t.Errorf("Expected count=0 after remove, got %d", tracker.Count())
			}

			if tracker.Get(tt.removeID) != nil {
				t.Error("Shell should not exist after remove")
			}
		})
	}
}

func TestShellTracker_Cleanup(t *testing.T) {
	type shellSpec struct {
		id           string
		state        domain.ShellState
		startedAgo   time.Duration
		completedAgo time.Duration
	}

	tests := []struct {
		name          string
		shells        []shellSpec
		maxAge        time.Duration
		wantRemoved   int
		wantCount     int
		wantGone      []string
		wantRemaining []string
	}{
		{
			name: "removes old completed shells only",
			shells: []shellSpec{
				{id: "old-shell", state: domain.ShellStateCompleted, completedAgo: 2 * time.Hour},
				{id: "recent-shell", state: domain.ShellStateCompleted, completedAgo: 5 * time.Minute},
			},
			maxAge:        1 * time.Hour,
			wantRemoved:   1,
			wantCount:     1,
			wantGone:      []string{"old-shell"},
			wantRemaining: []string{"recent-shell"},
		},
		{
			name: "does not remove running shells",
			shells: []shellSpec{
				{id: "old-running", state: domain.ShellStateRunning, startedAgo: 2 * time.Hour},
			},
			maxAge:      1 * time.Hour,
			wantRemoved: 0,
			wantCount:   1,
		},
		{
			name: "removes all terminal states",
			shells: []shellSpec{
				{id: "completed", state: domain.ShellStateCompleted, completedAgo: 2 * time.Hour},
				{id: "failed", state: domain.ShellStateFailed, completedAgo: 2 * time.Hour},
				{id: "cancelled", state: domain.ShellStateCancelled, completedAgo: 2 * time.Hour},
			},
			maxAge:      1 * time.Hour,
			wantRemoved: 3,
			wantCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewShellTracker(10)
			for _, spec := range tt.shells {
				shell := createTestShell(spec.id, spec.state)
				if spec.startedAgo != 0 {
					shell.StartedAt = time.Now().Add(-spec.startedAgo)
				}
				if spec.completedAgo != 0 {
					completedAt := time.Now().Add(-spec.completedAgo)
					shell.CompletedAt = &completedAt
				}
				mustAddShell(t, tracker, shell)
			}

			removed := tracker.Cleanup(tt.maxAge)

			if removed != tt.wantRemoved {
				t.Errorf("Expected %d shells removed, got %d", tt.wantRemoved, removed)
			}

			if tracker.Count() != tt.wantCount {
				t.Errorf("Expected count=%d after cleanup, got %d", tt.wantCount, tracker.Count())
			}

			for _, id := range tt.wantGone {
				if tracker.Get(id) != nil {
					t.Errorf("Shell %s should have been removed", id)
				}
			}

			for _, id := range tt.wantRemaining {
				if tracker.Get(id) == nil {
					t.Errorf("Shell %s should still exist", id)
				}
			}
		})
	}
}

func TestCountRunning(t *testing.T) {
	tracker := NewShellTracker(10)

	mustAddShell(t, tracker, createTestShell("running-1", domain.ShellStateRunning))
	mustAddShell(t, tracker, createTestShell("running-2", domain.ShellStateRunning))
	mustAddShell(t, tracker, createTestShell("completed-1", domain.ShellStateCompleted))
	mustAddShell(t, tracker, createTestShell("failed-1", domain.ShellStateFailed))
	mustAddShell(t, tracker, createTestShell("cancelled-1", domain.ShellStateCancelled))

	if tracker.Count() != 5 {
		t.Errorf("Expected count=5, got %d", tracker.Count())
	}

	if tracker.CountRunning() != 2 {
		t.Errorf("Expected countRunning=2, got %d", tracker.CountRunning())
	}
}

func TestConcurrentAccess(t *testing.T) {
	maxConcurrent := 50
	tracker := NewShellTracker(maxConcurrent)

	var wg sync.WaitGroup
	numGoroutines := 10
	operationsPerGoroutine := 10

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				shell := createTestShell(fmt.Sprintf("shell-%d-%d", id, j), domain.ShellStateRunning)
				_ = tracker.Add(shell)
			}
		}(i)
	}

	wg.Wait()

	count := tracker.Count()
	if count == 0 {
		t.Error("Expected some shells to be added")
	}
	if count > maxConcurrent {
		t.Errorf("Expected count to not exceed max concurrent limit of %d, got %d", maxConcurrent, count)
	}

	t.Logf("Successfully added %d shells concurrently (max: %d)", count, maxConcurrent)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				tracker.GetAll()
				tracker.Count()
				tracker.CountRunning()
			}
		}()
	}

	wg.Wait()

	t.Log("Concurrent reads completed without crashes")
}

func TestConcurrentAddRemove(t *testing.T) {
	tracker := NewShellTracker(20)

	var wg sync.WaitGroup
	done := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()

		i := 0
		for {
			select {
			case <-done:
				return
			default:
				shell := createTestShell(fmt.Sprintf("shell-%d", i), domain.ShellStateRunning)
				if err := tracker.Add(shell); err != nil {
					t.Errorf("Add failed: %v", err)
					return
				}
				i++
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-done:
				return
			default:
				shells := tracker.GetAll()
				if len(shells) > 0 {
					if err := tracker.Remove(shells[0].ShellID); err != nil {
						t.Errorf("Remove failed: %v", err)
						return
					}
				}
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(done)

	wg.Wait()

	t.Logf("Concurrent add/remove completed, final count: %d", tracker.Count())
}

func TestShellState_IsTerminal(t *testing.T) {
	tests := []struct {
		state      domain.ShellState
		isTerminal bool
	}{
		{domain.ShellStateRunning, false},
		{domain.ShellStateCompleted, true},
		{domain.ShellStateFailed, true},
		{domain.ShellStateCancelled, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			result := tt.state.IsTerminal()

			if result != tt.isTerminal {
				t.Errorf("Expected %s.IsTerminal()=%v, got %v", tt.state, tt.isTerminal, result)
			}
		})
	}
}
