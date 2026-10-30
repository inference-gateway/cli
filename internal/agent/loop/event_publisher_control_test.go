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
			p := newEventPublisher("req-1", events, nil)

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

// TestSendAfterCancel verifies a cancelled run still delivers to a draining
// consumer and never wedges on one that stopped reading.
func TestSendAfterCancel(t *testing.T) {
	cancelled := make(chan struct{})
	close(cancelled)

	tests := []struct {
		name   string
		buffer int
		check  func(t *testing.T, p *eventPublisher, events chan agentdomain.ChatEvent)
	}{
		{
			name:   "published after cancel reaches a draining consumer",
			buffer: 1,
			check: func(t *testing.T, p *eventPublisher, events chan agentdomain.ChatEvent) {
				for range 100 {
					p.publishChatCancelled(t.Context(), nil)
					select {
					case got := <-events:
						if complete, ok := got.(agentdomain.ChatCompleteEvent); !ok || !complete.Cancelled {
							t.Fatalf("event = %#v, want a cancelled ChatCompleteEvent", got)
						}
					default:
						t.Fatal("the cancelled event was dropped although the consumer had room")
					}
				}
			},
		},
		{
			name:   "an unread channel unblocks on cancel",
			buffer: 0,
			check: func(t *testing.T, p *eventPublisher, _ chan agentdomain.ChatEvent) {
				returned := make(chan struct{})
				go func() {
					p.publishChatStart()
					close(returned)
				}()
				select {
				case <-returned:
				case <-time.After(2 * time.Second):
					t.Fatal("send wedged on a cancelled run with no consumer")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := make(chan agentdomain.ChatEvent, tt.buffer)
			tt.check(t, newEventPublisher("req-1", events, cancelled), events)
		})
	}
}
