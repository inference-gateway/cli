package agent

import (
	"context"
	"testing"
	"time"

	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
)

// TestWaitForBackgroundTasks covers the headless completion boundary: a run
// with pending background work must block until the work posts a result to
// the queue (or quiesces), and return immediately when nothing is pending.
func TestWaitForBackgroundTasks(t *testing.T) {
	t.Run("returns immediately when nothing pending", func(t *testing.T) {
		registry := &schedmocks.FakeBackgroundTaskRegistry{}
		registry.HasPendingReturns(false)
		s := &AgentServiceImpl{bgRegistry: registry, messageQueue: &convmocks.FakeMessageQueue{}}

		done := make(chan struct{})
		go func() { s.waitForBackgroundTasks(context.Background()); close(done) }()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("waitForBackgroundTasks blocked with no pending tasks")
		}
	})

	t.Run("waits until queue receives a result", func(t *testing.T) {
		registry := &schedmocks.FakeBackgroundTaskRegistry{}
		registry.HasPendingReturns(true)
		queue := &convmocks.FakeMessageQueue{}
		queue.IsEmptyReturns(true)
		s := &AgentServiceImpl{bgRegistry: registry, messageQueue: queue}

		done := make(chan struct{})
		go func() { s.waitForBackgroundTasks(context.Background()); close(done) }()

		select {
		case <-done:
			t.Fatal("returned while task pending and queue empty")
		case <-time.After(200 * time.Millisecond):
		}

		queue.IsEmptyReturns(false) // background task posted its result
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("did not return after queue received a result")
		}
	})

	t.Run("cancellation unblocks", func(t *testing.T) {
		registry := &schedmocks.FakeBackgroundTaskRegistry{}
		registry.HasPendingReturns(true)
		queue := &convmocks.FakeMessageQueue{}
		queue.IsEmptyReturns(true)
		s := &AgentServiceImpl{bgRegistry: registry, messageQueue: queue}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { s.waitForBackgroundTasks(ctx); close(done) }()
		cancel()
		select {
		case <-done:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("did not return on context cancellation")
		}
	})
}
