package process

import (
	"errors"
	"os"
	"testing"
)

func TestStartedByWalksUpTheParentChain(t *testing.T) {
	parents := map[int]int{40: 30, 30: 20, 20: 1, 50: 1}
	parentOf := func(pid int) (int, error) {
		parent, known := parents[pid]
		if !known {
			return 0, errors.New("no such process")
		}
		return parent, nil
	}
	tests := []struct {
		name     string
		pid      int
		ancestor int
		want     bool
	}{
		{"the process itself", 20, 20, true},
		{"a child", 30, 20, true},
		{"a grandchild", 40, 20, true},
		{"an unrelated process", 50, 20, false},
		{"a parent is not started by its child", 20, 30, false},
		{"a process that is gone", 99, 20, false},
		{"no owner", 0, 20, false},
		{"init is nobody's descendant", 1, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := startedBy(tt.pid, tt.ancestor, parentOf); got != tt.want {
				t.Fatalf("startedBy(%d, %d) = %v, want %v", tt.pid, tt.ancestor, got, tt.want)
			}
		})
	}
}

func TestStartedByGivesUpOnAParentLoop(t *testing.T) {
	loop := func(pid int) (int, error) { return pid + 1, nil }
	if startedBy(10, 5, loop) {
		t.Fatal("a chain that never reaches the ancestor must not match")
	}
}

func TestStartedByFindsThisProcessUnderItsParent(t *testing.T) {
	if !StartedBy(t.Context(), os.Getpid(), os.Getppid()) {
		t.Fatal("this process was started by its parent")
	}
	if StartedBy(t.Context(), os.Getppid(), os.Getpid()) {
		t.Fatal("the parent was not started by this process")
	}
}
