package utils

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	require "github.com/stretchr/testify/require"
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

func TestAdd_Success(t *testing.T) {
	tracker := NewShellTracker(5)

	shell := createTestShell("shell-123", domain.ShellStateRunning)

	err := tracker.Add(shell)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if tracker.Count() != 1 {
		t.Errorf("Expected count=1, got %d", tracker.Count())
	}

	if tracker.CountRunning() != 1 {
		t.Errorf("Expected countRunning=1, got %d", tracker.CountRunning())
	}
}

func TestAdd_DuplicateID(t *testing.T) {
	tracker := NewShellTracker(5)

	shell1 := createTestShell("shell-123", domain.ShellStateRunning)
	shell2 := createTestShell("shell-123", domain.ShellStateRunning)

	err := tracker.Add(shell1)
	if err != nil {
		t.Fatalf("First Add failed: %v", err)
	}

	err = tracker.Add(shell2)
	if err == nil {
		t.Fatal("Expected error when adding duplicate ID, got nil")
	}

	if tracker.Count() != 1 {
		t.Errorf("Expected count=1 after duplicate, got %d", tracker.Count())
	}
}

func TestAdd_MaxConcurrentLimit(t *testing.T) {
	tracker := NewShellTracker(3)

	for i := 0; i < 3; i++ {
		shell := createTestShell(fmt.Sprintf("shell-%d", i), domain.ShellStateRunning)
		err := tracker.Add(shell)
		if err != nil {
			t.Fatalf("Add shell %d failed: %v", i, err)
		}
	}

	shell4 := createTestShell("shell-4", domain.ShellStateRunning)
	err := tracker.Add(shell4)
	if err == nil {
		t.Fatal("Expected error when exceeding max concurrent, got nil")
	}

	if tracker.CountRunning() != 3 {
		t.Errorf("Expected countRunning=3, got %d", tracker.CountRunning())
	}
}

func TestAdd_MaxConcurrentLimit_AllowsCompleted(t *testing.T) {
	tracker := NewShellTracker(2)

	for i := 0; i < 2; i++ {
		shell := createTestShell(fmt.Sprintf("shell-running-%d", i), domain.ShellStateRunning)
		err := tracker.Add(shell)
		if err != nil {
			t.Fatalf("Add running shell %d failed: %v", i, err)
		}
	}

	shell := createTestShell("shell-completed", domain.ShellStateCompleted)
	err := tracker.Add(shell)
	if err != nil {
		t.Fatalf("Should allow adding completed shell: %v", err)
	}

	if tracker.Count() != 3 {
		t.Errorf("Expected count=3, got %d", tracker.Count())
	}

	if tracker.CountRunning() != 2 {
		t.Errorf("Expected countRunning=2, got %d", tracker.CountRunning())
	}
}

func TestGet_Success(t *testing.T) {
	tracker := NewShellTracker(5)

	shell := createTestShell("shell-123", domain.ShellStateRunning)
	require.NoError(t, tracker.Add(shell))

	retrieved := tracker.Get("shell-123")

	if retrieved == nil {
		t.Fatal("Get returned nil")
	}

	if retrieved.ShellID != "shell-123" {
		t.Errorf("Expected ShellID=shell-123, got %s", retrieved.ShellID)
	}
}

func TestGet_NotFound(t *testing.T) {
	tracker := NewShellTracker(5)

	retrieved := tracker.Get("nonexistent")

	if retrieved != nil {
		t.Error("Expected Get to return nil for nonexistent shell")
	}
}

func TestGetAll(t *testing.T) {
	tracker := NewShellTracker(5)

	for i := 0; i < 3; i++ {
		shell := createTestShell(fmt.Sprintf("shell-%d", i), domain.ShellStateRunning)
		require.NoError(t, tracker.Add(shell))
	}

	all := tracker.GetAll()

	if len(all) != 3 {
		t.Errorf("Expected 3 shells, got %d", len(all))
	}
}

func TestGetAll_Empty(t *testing.T) {
	tracker := NewShellTracker(5)

	all := tracker.GetAll()

	if len(all) != 0 {
		t.Errorf("Expected empty slice, got %d shells", len(all))
	}
}

func TestRemove_Success(t *testing.T) {
	tracker := NewShellTracker(5)

	shell := createTestShell("shell-123", domain.ShellStateRunning)
	require.NoError(t, tracker.Add(shell))

	err := tracker.Remove("shell-123")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if tracker.Count() != 0 {
		t.Errorf("Expected count=0 after remove, got %d", tracker.Count())
	}

	retrieved := tracker.Get("shell-123")
	if retrieved != nil {
		t.Error("Shell should not exist after remove")
	}
}

func TestRemove_NotFound(t *testing.T) {
	tracker := NewShellTracker(5)

	err := tracker.Remove("nonexistent")
	if err == nil {
		t.Fatal("Expected error when removing nonexistent shell, got nil")
	}
}

func TestCleanup_RemovesOldCompletedShells(t *testing.T) {
	tracker := NewShellTracker(5)

	oldShell := createTestShell("old-shell", domain.ShellStateCompleted)
	oldTime := time.Now().Add(-2 * time.Hour)
	oldShell.CompletedAt = &oldTime
	require.NoError(t, tracker.Add(oldShell))

	recentShell := createTestShell("recent-shell", domain.ShellStateCompleted)
	recentTime := time.Now().Add(-5 * time.Minute)
	recentShell.CompletedAt = &recentTime
	require.NoError(t, tracker.Add(recentShell))

	removed := tracker.Cleanup(1 * time.Hour)

	if removed != 1 {
		t.Errorf("Expected 1 shell removed, got %d", removed)
	}

	if tracker.Count() != 1 {
		t.Errorf("Expected count=1 after cleanup, got %d", tracker.Count())
	}

	if tracker.Get("old-shell") != nil {
		t.Error("Old shell should have been removed")
	}

	if tracker.Get("recent-shell") == nil {
		t.Error("Recent shell should still exist")
	}
}

func TestCleanup_DoesNotRemoveRunningShells(t *testing.T) {
	tracker := NewShellTracker(5)

	oldRunning := createTestShell("old-running", domain.ShellStateRunning)
	oldRunning.StartedAt = time.Now().Add(-2 * time.Hour)
	require.NoError(t, tracker.Add(oldRunning))

	removed := tracker.Cleanup(1 * time.Hour)

	if removed != 0 {
		t.Errorf("Expected 0 shells removed (running shells not cleaned), got %d", removed)
	}

	if tracker.Count() != 1 {
		t.Errorf("Expected count=1, got %d", tracker.Count())
	}
}

func TestCleanup_AllTerminalStates(t *testing.T) {
	tracker := NewShellTracker(10)

	oldTime := time.Now().Add(-2 * time.Hour)

	completedShell := createTestShell("completed", domain.ShellStateCompleted)
	completedShell.CompletedAt = &oldTime
	require.NoError(t, tracker.Add(completedShell))

	failedShell := createTestShell("failed", domain.ShellStateFailed)
	failedShell.CompletedAt = &oldTime
	require.NoError(t, tracker.Add(failedShell))

	cancelledShell := createTestShell("cancelled", domain.ShellStateCancelled)
	cancelledShell.CompletedAt = &oldTime
	require.NoError(t, tracker.Add(cancelledShell))

	removed := tracker.Cleanup(1 * time.Hour)

	if removed != 3 {
		t.Errorf("Expected 3 shells removed, got %d", removed)
	}

	if tracker.Count() != 0 {
		t.Errorf("Expected count=0, got %d", tracker.Count())
	}
}

func TestCountRunning(t *testing.T) {
	tracker := NewShellTracker(10)

	require.NoError(t, tracker.Add(createTestShell("running-1", domain.ShellStateRunning)))
	require.NoError(t, tracker.Add(createTestShell("running-2", domain.ShellStateRunning)))
	require.NoError(t, tracker.Add(createTestShell("completed-1", domain.ShellStateCompleted)))
	require.NoError(t, tracker.Add(createTestShell("failed-1", domain.ShellStateFailed)))
	require.NoError(t, tracker.Add(createTestShell("cancelled-1", domain.ShellStateCancelled)))

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
				require.NoError(t, tracker.Add(shell))
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
					err := tracker.Remove(shells[0].ShellID)
					require.NoError(t, err)
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
