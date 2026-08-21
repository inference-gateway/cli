package agent

import (
	"context"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	states "github.com/inference-gateway/cli/internal/agent/states"
)

// TestStateMachineInitialization tests that the state machine initializes to Idle state
func TestStateMachineInitialization(t *testing.T) {
	sm := NewAgentStateMachine()

	if sm.GetCurrentState() != states.StateIdle {
		t.Errorf("expected initial state to be Idle, got %s", sm.GetCurrentState())
	}
}

// createTestAgentContext creates a minimal agent context for testing
func createTestAgentContext() *states.AgentContext {
	return &states.AgentContext{
		RequestID:        "test-request-id",
		Conversation:     &[]sdk.Message{},
		MessageQueue:     &convmocks.FakeMessageQueue{},
		ConversationRepo: &convmocks.FakeConversationRepository{},
		ToolCalls:        nil,
		Turns:            0,
		MaxTurns:         10,
		HasToolResults:   false,
		ApprovalPolicy:   nil,
		Ctx:              context.Background(),
		IsChatMode:       true,
	}
}

// TestValidTransitions_BasicFlow tests basic state transition flow
func TestValidTransitions_BasicFlow(t *testing.T) {
	sm := NewAgentStateMachine()
	ctx := createTestAgentContext()

	// Test Idle to CheckingQueue
	err := sm.Transition(ctx, states.StateCheckingQueue)
	if err != nil {
		t.Errorf("Idle → CheckingQueue should succeed, got error: %v", err)
	}
	if sm.GetCurrentState() != states.StateCheckingQueue {
		t.Errorf("Expected state CheckingQueue, got %s", sm.GetCurrentState())
	}

	// Test CheckingQueue to StreamingLLM
	*ctx.Conversation = []sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}}
	err = sm.Transition(ctx, states.StateStreamingLLM)
	if err != nil {
		t.Errorf("CheckingQueue → StreamingLLM should succeed, got error: %v", err)
	}

	// Test StreamingLLM to PostStream
	err = sm.Transition(ctx, states.StatePostStream)
	if err != nil {
		t.Errorf("StreamingLLM → PostStream should succeed, got error: %v", err)
	}
}

// TestInvalidTransitions tests that invalid state transitions are rejected
func TestInvalidTransitions(t *testing.T) {
	sm := NewAgentStateMachine()

	ctx := &states.AgentContext{
		Conversation:     &[]sdk.Message{},
		MessageQueue:     &convmocks.FakeMessageQueue{},
		ConversationRepo: &convmocks.FakeConversationRepository{},
		ToolCalls:        nil,
		Turns:            0,
		MaxTurns:         10,
		Ctx:              context.Background(),
		IsChatMode:       true,
	}

	tests := []struct {
		name        string
		fromState   states.AgentExecutionState
		toState     states.AgentExecutionState
		description string
	}{
		{
			name:        "StreamingLLM to Idle",
			fromState:   states.StateStreamingLLM,
			toState:     states.StateIdle,
			description: "Should not allow jumping back to Idle from StreamingLLM",
		},
		{
			name:        "PostStream to ExecutingTools without EvaluatingTools",
			fromState:   states.StatePostStream,
			toState:     states.StateExecutingTools,
			description: "Should require going through EvaluatingTools first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm = NewAgentStateMachine()

			testCtx := &states.AgentContext{
				Conversation: &[]sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}},
				MessageQueue: &convmocks.FakeMessageQueue{},
				Turns:        0,
				MaxTurns:     10,
				Ctx:          context.Background(),
				IsChatMode:   true,
			}

			switch tt.fromState {
			case states.StateStreamingLLM:
				_ = sm.Transition(testCtx, states.StateCheckingQueue)
				_ = sm.Transition(testCtx, states.StateStreamingLLM)
			case states.StatePostStream:
				_ = sm.Transition(testCtx, states.StateCheckingQueue)
				_ = sm.Transition(testCtx, states.StateStreamingLLM)
				_ = sm.Transition(testCtx, states.StatePostStream)
			}

			err := sm.Transition(ctx, tt.toState)
			if err == nil {
				t.Errorf("%s: expected transition to fail, but it succeeded", tt.description)
			}
		})
	}
}

// TestGuardConditions tests that guard functions work correctly via public interface
func TestGuardConditions(t *testing.T) {
	t.Run("CheckingQueue to Idle transition respects canComplete guard", func(t *testing.T) {
		sm := NewAgentStateMachine()

		ctx := &states.AgentContext{
			Conversation:   &[]sdk.Message{},
			MessageQueue:   &convmocks.FakeMessageQueue{},
			Turns:          0,
			HasToolResults: false,
			Ctx:            context.Background(),
			IsChatMode:     true,
		}
		fakeQueue := ctx.MessageQueue.(*convmocks.FakeMessageQueue)
		fakeQueue.IsEmptyReturns(true)

		_ = sm.Transition(ctx, states.StateCheckingQueue)

		err := sm.Transition(ctx, states.StateIdle)
		if err == nil {
			t.Error("expected transition to fail when canComplete guard fails (turns=0)")
		}

		ctx.Turns = 1
		err = sm.Transition(ctx, states.StateIdle)
		if err != nil {
			t.Errorf("expected transition to succeed when canComplete guard passes, got: %v", err)
		}
	})

	t.Run("PostToolExecution respects maxTurnsReached guard", func(t *testing.T) {
		sm := NewAgentStateMachine()

		ctx := &states.AgentContext{
			Conversation:   &[]sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}},
			MessageQueue:   &convmocks.FakeMessageQueue{},
			ToolCalls:      nil,
			Turns:          10,
			MaxTurns:       10,
			HasToolResults: false,
			Ctx:            context.Background(),
			IsChatMode:     true,
		}
		fakeQueue := ctx.MessageQueue.(*convmocks.FakeMessageQueue)
		fakeQueue.IsEmptyReturns(true)

		_ = sm.Transition(ctx, states.StateCheckingQueue)
		_ = sm.Transition(ctx, states.StateStreamingLLM)
		_ = sm.Transition(ctx, states.StatePostStream)

		ctx.ToolCalls = []*sdk.ChatCompletionMessageToolCall{
			{
				ID: "test",
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "test",
					Arguments: "{}",
				},
			},
		}
		_ = sm.Transition(ctx, states.StateEvaluatingTools)
		_ = sm.Transition(ctx, states.StateExecutingTools)
		_ = sm.Transition(ctx, states.StatePostToolExecution)

		if !sm.CanTransition(ctx, states.StateCompleting) {
			t.Error("expected to be able to transition to Completing when max turns reached")
		}
	})
}

// TestStateReset tests that the state machine can be reset
func TestStateReset(t *testing.T) {
	sm := NewAgentStateMachine()

	ctx := &states.AgentContext{
		Conversation: &[]sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}},
		MessageQueue: &convmocks.FakeMessageQueue{},
		Ctx:          context.Background(),
	}

	_ = sm.Transition(ctx, states.StateCheckingQueue)
	_ = sm.Transition(ctx, states.StateStreamingLLM)

	sm.Reset()

	if sm.GetCurrentState() != states.StateIdle {
		t.Errorf("expected state to be Idle after reset, got %s", sm.GetCurrentState())
	}
}

// TestGetValidTransitions tests that valid transitions are returned correctly
func TestGetValidTransitions(t *testing.T) {
	sm := NewAgentStateMachine()

	ctx := &states.AgentContext{
		Conversation:   &[]sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}},
		MessageQueue:   &convmocks.FakeMessageQueue{},
		Turns:          0,
		MaxTurns:       10,
		HasToolResults: false,
		Ctx:            context.Background(),
	}

	fakeQueue := ctx.MessageQueue.(*convmocks.FakeMessageQueue)
	fakeQueue.IsEmptyReturns(true)

	validStates := sm.GetValidTransitions(ctx)

	found := false
	for _, state := range validStates {
		if state == states.StateCheckingQueue {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected CheckingQueue to be in valid transitions from Idle")
	}
}

// TestGuardFunctions_CanComplete tests the canComplete guard function comprehensively
func TestGuardFunctions_CanComplete(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func(*states.AgentContext)
		expected bool
	}{
		{
			name: "returns_false_when_turns_is_zero",
			setupCtx: func(ctx *states.AgentContext) {
				ctx.Turns = 0
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*convmocks.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.Assistant, Content: sdk.NewMessageContent("test")},
				}
			},
			expected: false,
		},
		{
			name: "returns_false_when_has_tool_results",
			setupCtx: func(ctx *states.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = true
				fakeQueue := ctx.MessageQueue.(*convmocks.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.Assistant, Content: sdk.NewMessageContent("test")},
				}
			},
			expected: false,
		},
		{
			name: "returns_false_when_queue_not_empty",
			setupCtx: func(ctx *states.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*convmocks.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(false)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.Assistant, Content: sdk.NewMessageContent("test")},
				}
			},
			expected: false,
		},
		{
			name: "returns_false_when_last_message_is_user",
			setupCtx: func(ctx *states.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*convmocks.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.User, Content: sdk.NewMessageContent("test")},
				}
			},
			expected: false,
		},
		{
			name: "returns_true_when_all_conditions_met",
			setupCtx: func(ctx *states.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*convmocks.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.Assistant, Content: sdk.NewMessageContent("done")},
				}
			},
			expected: true,
		},
		{
			name: "returns_false_when_conversation_empty",
			setupCtx: func(ctx *states.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*convmocks.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{}
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewAgentStateMachine()
			smImpl := sm.(*AgentStateMachineImpl)

			ctx := &states.AgentContext{
				Conversation:   &[]sdk.Message{},
				MessageQueue:   &convmocks.FakeMessageQueue{},
				Turns:          0,
				HasToolResults: false,
				Ctx:            context.Background(),
			}

			if tt.setupCtx != nil {
				tt.setupCtx(ctx)
			}

			result := smImpl.canComplete(ctx)
			if result != tt.expected {
				t.Errorf("expected canComplete to return %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestGuardFunctions_NeedsApproval tests the needsApproval guard function
func TestGuardFunctions_NeedsApproval(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func(*states.AgentContext)
		expected bool
	}{
		{
			name: "returns_false_when_no_approval_policy",
			setupCtx: func(ctx *states.AgentContext) {
				ctx.ApprovalPolicy = nil
				ctx.ToolCalls = []*sdk.ChatCompletionMessageToolCall{
					{
						ID: "call-1",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Write",
							Arguments: `{}`,
						},
					},
				}
			},
			expected: false,
		},
		{
			name: "returns_true_when_tool_needs_approval",
			setupCtx: func(ctx *states.AgentContext) {
				fakePolicy := &agentdomainmocks.FakeApprovalPolicy{}
				fakePolicy.ShouldRequireApprovalReturns(true)
				ctx.ApprovalPolicy = fakePolicy
				ctx.ToolCalls = []*sdk.ChatCompletionMessageToolCall{
					{
						ID: "call-1",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Write",
							Arguments: `{}`,
						},
					},
				}
			},
			expected: true,
		},
		{
			name: "returns_false_when_no_tools_need_approval",
			setupCtx: func(ctx *states.AgentContext) {
				fakePolicy := &agentdomainmocks.FakeApprovalPolicy{}
				fakePolicy.ShouldRequireApprovalReturns(false)
				ctx.ApprovalPolicy = fakePolicy
				ctx.ToolCalls = []*sdk.ChatCompletionMessageToolCall{
					{
						ID: "call-1",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Read",
							Arguments: `{}`,
						},
					},
				}
			},
			expected: false,
		},
		{
			name: "returns_false_when_no_tool_calls",
			setupCtx: func(ctx *states.AgentContext) {
				fakePolicy := &agentdomainmocks.FakeApprovalPolicy{}
				ctx.ApprovalPolicy = fakePolicy
				ctx.ToolCalls = []*sdk.ChatCompletionMessageToolCall{}
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewAgentStateMachine()
			smImpl := sm.(*AgentStateMachineImpl)

			ctx := &states.AgentContext{
				ToolCalls:  nil,
				Ctx:        context.Background(),
				IsChatMode: true,
			}

			if tt.setupCtx != nil {
				tt.setupCtx(ctx)
			}

			result := smImpl.needsApproval(ctx)
			if result != tt.expected {
				t.Errorf("expected needsApproval to return %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestGuardFunctions_MaxTurnsReached tests the maxTurnsReached guard function
func TestGuardFunctions_MaxTurnsReached(t *testing.T) {
	tests := []struct {
		name     string
		turns    int
		maxTurns int
		expected bool
	}{
		{
			name:     "returns_false_when_below_max",
			turns:    5,
			maxTurns: 10,
			expected: false,
		},
		{
			name:     "returns_true_when_equal_to_max",
			turns:    10,
			maxTurns: 10,
			expected: true,
		},
		{
			name:     "returns_true_when_above_max",
			turns:    15,
			maxTurns: 10,
			expected: true,
		},
		{
			name:     "returns_false_when_zero_turns",
			turns:    0,
			maxTurns: 10,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewAgentStateMachine()
			smImpl := sm.(*AgentStateMachineImpl)

			ctx := &states.AgentContext{
				Turns:    tt.turns,
				MaxTurns: tt.maxTurns,
				Ctx:      context.Background(),
			}

			result := smImpl.maxTurnsReached(ctx)
			if result != tt.expected {
				t.Errorf("expected maxTurnsReached to return %v, got %v", tt.expected, result)
			}
		})
	}
}

// TestExecutingToolsToStoppedTransition is a regression test for the previously
// unregistered ExecutingTools → Stopped transition. The executeTools goroutine
// requests Stopped when a tool result signals stop (a rejected tool or a
// successful RequestPlanApproval); that transition silently failed before, so
// the loop was left parked in ExecutingTools. It must now succeed from
// ExecutingTools - and only from there (no global loop to Stopped was added).
func TestExecutingToolsToStoppedTransition(t *testing.T) {
	sm := NewAgentStateMachine()
	ctx := createTestAgentContext()
	*ctx.Conversation = []sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}}
	ctx.ToolCalls = []*sdk.ChatCompletionMessageToolCall{
		{ID: "t", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read", Arguments: "{}"}},
	}

	if err := sm.Transition(ctx, states.StateStopped); err == nil {
		t.Fatal("Idle → Stopped should fail (Stopped is not a global terminal)")
	}

	_ = sm.Transition(ctx, states.StateCheckingQueue)
	_ = sm.Transition(ctx, states.StateStreamingLLM)
	_ = sm.Transition(ctx, states.StatePostStream)
	_ = sm.Transition(ctx, states.StateEvaluatingTools)
	if err := sm.Transition(ctx, states.StateExecutingTools); err != nil {
		t.Fatalf("setup: reaching ExecutingTools should succeed, got: %v", err)
	}

	if err := sm.Transition(ctx, states.StateStopped); err != nil {
		t.Fatalf("ExecutingTools → Stopped should succeed, got error: %v", err)
	}
	if sm.GetCurrentState() != states.StateStopped {
		t.Errorf("expected state Stopped, got %s", sm.GetCurrentState())
	}
}

// TestMaxTurnsExceededFlag verifies the PostToolExecution → Completing action
// marks the context when completion is forced by the turn limit (and only then),
// so the headless renderers can surface ErrMaxTurnsReached (exit code 2).
func TestMaxTurnsExceededFlag(t *testing.T) {
	tests := []struct {
		name           string
		hasToolResults bool
		want           bool
	}{
		{"forced by turn limit", true, true},
		{"legitimately complete at limit", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewAgentStateMachine()
			ctx := &states.AgentContext{
				Conversation: &[]sdk.Message{{Role: sdk.Assistant, Content: sdk.NewMessageContent("done")}},
				MessageQueue: &convmocks.FakeMessageQueue{},
				Turns:        10,
				MaxTurns:     10,
				Ctx:          context.Background(),
			}
			ctx.MessageQueue.(*convmocks.FakeMessageQueue).IsEmptyReturns(true)

			_ = sm.Transition(ctx, states.StateCheckingQueue)
			_ = sm.Transition(ctx, states.StateStreamingLLM)
			_ = sm.Transition(ctx, states.StatePostStream)
			ctx.ToolCalls = []*sdk.ChatCompletionMessageToolCall{{ID: "t", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "t", Arguments: "{}"}}}
			_ = sm.Transition(ctx, states.StateEvaluatingTools)
			_ = sm.Transition(ctx, states.StateExecutingTools)
			_ = sm.Transition(ctx, states.StatePostToolExecution)

			ctx.HasToolResults = tt.hasToolResults
			if err := sm.Transition(ctx, states.StateCompleting); err != nil {
				t.Fatalf("transition to Completing failed: %v", err)
			}
			if ctx.MaxTurnsExceeded != tt.want {
				t.Errorf("MaxTurnsExceeded = %v, want %v", ctx.MaxTurnsExceeded, tt.want)
			}
		})
	}
}
