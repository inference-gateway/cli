package loop

import (
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	states "github.com/inference-gateway/cli/internal/agent/loop/states"
)

func agentExecutingTools(t *testing.T) (*eventDrivenAgent, *convmocks.FakeConversationRepository) {
	t.Helper()
	ctx := createTestAgentContext()
	*ctx.Conversation = []sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}}
	ctx.ToolCalls = []*sdk.ChatCompletionMessageToolCall{{ID: "t"}}

	agent := &eventDrivenAgent{stateMachine: newAgentStateMachine(), agentCtx: ctx}
	for _, state := range []states.AgentExecutionState{
		states.StateCheckingQueue, states.StateStreamingLLM, states.StatePostStream,
		states.StateEvaluatingTools, states.StateExecutingTools,
	} {
		if err := agent.stateMachine.Transition(ctx, state); err != nil {
			t.Fatalf("setup: reaching %s: %v", state, err)
		}
	}
	return agent, ctx.ConversationRepo.(*convmocks.FakeConversationRepository)
}

func TestAwaitUser_PausesTheWorkingTimeUntilTheLastPromptIsAnswered(t *testing.T) {
	agent, repo := agentExecutingTools(t)
	if repo.StartWorkingCallCount() != 1 {
		t.Fatalf("leaving Idle started the stopwatch %d times, want 1", repo.StartWorkingCallCount())
	}

	agent.awaitUser(func() {
		agent.awaitUser(func() {
			if got := agent.stateMachine.GetCurrentState(); got != states.StateInputRequired {
				t.Errorf("with a prompt open got %s, want InputRequired", got)
			}
		})
		if got := agent.stateMachine.GetCurrentState(); got != states.StateInputRequired {
			t.Errorf("with one prompt still open got %s, want InputRequired", got)
		}
	})

	if got := agent.stateMachine.GetCurrentState(); got != states.StateExecutingTools {
		t.Errorf("after the answers got %s, want the suspended ExecutingTools", got)
	}
	if repo.StopWorkingCallCount() != 1 || repo.StartWorkingCallCount() != 2 {
		t.Errorf("stopwatch stopped %d and started %d times, want one pause and one resume",
			repo.StopWorkingCallCount(), repo.StartWorkingCallCount())
	}
}

func TestAwaitUser_LeavesACancelledRunCancelled(t *testing.T) {
	agent, repo := agentExecutingTools(t)

	agent.awaitUser(func() {
		if err := agent.stateMachine.Transition(agent.agentCtx, states.StateCancelled); err != nil {
			t.Fatalf("InputRequired → Cancelled: %v", err)
		}
	})

	if got := agent.stateMachine.GetCurrentState(); got != states.StateCancelled {
		t.Errorf("got %s, want the run to stay Cancelled", got)
	}
	if repo.StartWorkingCallCount() != 1 {
		t.Error("a cancelled run must not resume the stopwatch")
	}
}
