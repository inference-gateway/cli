package loop

import (
	"fmt"
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// TestControlEventsWaitForTheConsumer verifies control events block on a full
// channel instead of being dropped, so a slow consumer never loses them.
func TestControlEventsWaitForTheConsumer(t *testing.T) {
	tests := []struct {
		name    string
		publish func(p *eventPublisher)
		want    agentdomain.ChatEvent
	}{
		{
			name: "tool execution completed",
			publish: func(p *eventPublisher) {
				p.publishToolExecutionCompleted([]convdomain.ConversationEntry{{ToolExecution: &agentdomain.ToolExecutionResult{Success: true}}})
			},
			want: agentdomain.ToolExecutionCompletedEvent{},
		},
		{
			name:    "plan approval request",
			publish: func(p *eventPublisher) { p.publishPlanApprovalRequest("plan", "plan-1") },
			want:    agentdomain.PlanApprovalRequestedEvent{},
		},
		{
			name:    "todo update",
			publish: func(p *eventPublisher) { p.publishTodoUpdate([]agentdomain.TodoItem{{Content: "x"}}) },
			want:    agentdomain.TodoUpdateChatEvent{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := make(chan agentdomain.ChatEvent)
			p := newEventPublisher("req-1", events)

			go tt.publish(p)

			select {
			case got := <-events:
				if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tt.want) {
					t.Fatalf("event type = %T, want %T", got, tt.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("control event was dropped instead of waiting for the consumer")
			}
		})
	}
}
