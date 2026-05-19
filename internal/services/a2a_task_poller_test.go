package services

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// TestA2ATaskPoller_WakeUpAgentOnEnqueue covers the regression that hung
// the CLI on "Processing queued message...": after the poller enqueues a
// result it must push MessageReceivedEvent{} into the agent's event channel
// so the Idle event loop unblocks.
func TestA2ATaskPoller_WakeUpAgentOnEnqueue(t *testing.T) {
	t.Run("wakes agent when channel is set", func(t *testing.T) {
		tracker := &domainmocks.FakeA2ATaskTracker{}
		queue := &domainmocks.FakeMessageQueue{}
		convRepo := &domainmocks.FakeConversationRepository{}
		convRepo.FormatToolResultForLLMReturns("formatted")

		chatEvents := make(chan domain.ChatEvent, 8)
		agentEvents := make(chan domain.AgentEvent, 1)

		p := NewA2ATaskPoller(tracker, chatEvents, queue, "req-1", convRepo)
		p.SetAgentEventChannel(agentEvents)

		p.addResultToMessageQueue("task-A", &domain.ToolExecutionResult{
			ToolName: "A2ATask",
			Success:  true,
		})

		select {
		case ev := <-agentEvents:
			if _, ok := ev.(domain.MessageReceivedEvent); !ok {
				t.Fatalf("expected MessageReceivedEvent, got %T", ev)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("expected wake-up event on agent channel, got none")
		}

		if got := queue.EnqueueCallCount(); got != 1 {
			t.Fatalf("expected message enqueued once, got %d", got)
		}
	})

	t.Run("no-op when channel is nil", func(t *testing.T) {
		tracker := &domainmocks.FakeA2ATaskTracker{}
		queue := &domainmocks.FakeMessageQueue{}
		convRepo := &domainmocks.FakeConversationRepository{}
		convRepo.FormatToolResultForLLMReturns("formatted")

		chatEvents := make(chan domain.ChatEvent, 8)

		p := NewA2ATaskPoller(tracker, chatEvents, queue, "req-1", convRepo)
		// SetAgentEventChannel intentionally not called.

		p.addResultToMessageQueue("task-B", &domain.ToolExecutionResult{
			ToolName: "A2ATask",
			Success:  true,
		})

		if got := queue.EnqueueCallCount(); got != 1 {
			t.Fatalf("expected message still enqueued, got %d", got)
		}
	})

	t.Run("drops wake-up when channel is full", func(t *testing.T) {
		tracker := &domainmocks.FakeA2ATaskTracker{}
		queue := &domainmocks.FakeMessageQueue{}
		convRepo := &domainmocks.FakeConversationRepository{}
		convRepo.FormatToolResultForLLMReturns("formatted")

		chatEvents := make(chan domain.ChatEvent, 8)
		agentEvents := make(chan domain.AgentEvent, 1)
		agentEvents <- domain.MessageReceivedEvent{}

		p := NewA2ATaskPoller(tracker, chatEvents, queue, "req-1", convRepo)
		p.SetAgentEventChannel(agentEvents)

		p.addResultToMessageQueue("task-C", &domain.ToolExecutionResult{
			ToolName: "A2ATask",
			Success:  true,
		})

		if got := queue.EnqueueCallCount(); got != 1 {
			t.Fatalf("expected message still enqueued, got %d", got)
		}
	})
}
