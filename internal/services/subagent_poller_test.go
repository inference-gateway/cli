package services

import (
	"context"
	"sync"
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	utils "github.com/inference-gateway/cli/internal/utils"
)

// When the poller stops (session ctx cancelled), any still-tracked subagent's
// CancelFunc must be called so detached (async) `infer agent` subprocesses are
// not left orphaned.
func TestSubagentPoller_StopCancelsDetachedSubagents(t *testing.T) {
	tr := utils.NewSubagentTracker()
	var mu sync.Mutex
	canceled := false
	_ = tr.AddSubagent(&domain.SubagentState{
		ID:         "x",
		Status:     domain.SubagentRunning,
		CancelFunc: func() { mu.Lock(); canceled = true; mu.Unlock() },
		ResultChan: make(chan *domain.ToolExecutionResult, 1),
		ErrorChan:  make(chan error, 1),
	})

	p := NewSubagentPoller(tr, nil, nil, "req", nil)
	ctx, cancel := context.WithCancel(context.Background())
	go p.Start(ctx)
	cancel() // ctx.Done triggers stopAllMonitors

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		done := canceled
		mu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected detached subagent CancelFunc to be called on poller stop")
}
