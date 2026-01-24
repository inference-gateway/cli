package services

import (
	"context"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	mockdomain "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
)

// TestStateMachineInitialization tests that the state machine initializes to Idle state
func TestStateMachineInitialization(t *testing.T) {
	stateManager := &mockdomain.FakeStateManager{}
	sm := NewAgentStateMachine(stateManager)

	if sm.GetCurrentState() != domain.StateIdle {
		t.Errorf("expected initial state to be Idle, got %s", sm.GetCurrentState())
	}
}

// createTestAgentContext creates a minimal agent context for testing
func createTestAgentContext() *domain.AgentContext {
	return &domain.AgentContext{
		Conversation:     &[]sdk.Message{},
		MessageQueue:     &mockdomain.FakeMessageQueue{},
		ConversationRepo: &mockdomain.FakeConversationRepository{},
		ToolCalls:        make(map[string]*sdk.ChatCompletionMessageToolCall),
		EventPublisher:   nil,
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
	stateManager := &mockdomain.FakeStateManager{}
	sm := NewAgentStateMachine(stateManager)
	ctx := createTestAgentContext()

	// Test Idle to CheckingQueue
	err := sm.Transition(ctx, domain.StateCheckingQueue)
	if err != nil {
		t.Errorf("Idle → CheckingQueue should succeed, got error: %v", err)
	}
	if sm.GetCurrentState() != domain.StateCheckingQueue {
		t.Errorf("Expected state CheckingQueue, got %s", sm.GetCurrentState())
	}

	// Test CheckingQueue to StreamingLLM
	*ctx.Conversation = []sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}}
	err = sm.Transition(ctx, domain.StateStreamingLLM)
	if err != nil {
		t.Errorf("CheckingQueue → StreamingLLM should succeed, got error: %v", err)
	}

	// Test StreamingLLM to PostStream
	err = sm.Transition(ctx, domain.StatePostStream)
	if err != nil {
		t.Errorf("StreamingLLM → PostStream should succeed, got error: %v", err)
	}
}

// TestInvalidTransitions tests that invalid state transitions are rejected
func TestInvalidTransitions(t *testing.T) {
	stateManager := &mockdomain.FakeStateManager{}
	sm := NewAgentStateMachine(stateManager)

	ctx := &domain.AgentContext{
		Conversation:     &[]sdk.Message{},
		MessageQueue:     &mockdomain.FakeMessageQueue{},
		ConversationRepo: &mockdomain.FakeConversationRepository{},
		ToolCalls:        make(map[string]*sdk.ChatCompletionMessageToolCall),
		Turns:            0,
		MaxTurns:         10,
		Ctx:              context.Background(),
		IsChatMode:       true,
	}

	tests := []struct {
		name        string
		fromState   domain.AgentExecutionState
		toState     domain.AgentExecutionState
		description string
	}{
		{
			name:        "StreamingLLM to Idle",
			fromState:   domain.StateStreamingLLM,
			toState:     domain.StateIdle,
			description: "Should not allow jumping back to Idle from StreamingLLM",
		},
		{
			name:        "PostStream to ExecutingTools without EvaluatingTools",
			fromState:   domain.StatePostStream,
			toState:     domain.StateExecutingTools,
			description: "Should require going through EvaluatingTools first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm = NewAgentStateMachine(&mockdomain.FakeStateManager{})

			testCtx := &domain.AgentContext{
				Conversation: &[]sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}},
				MessageQueue: &mockdomain.FakeMessageQueue{},
				Turns:        0,
				MaxTurns:     10,
				Ctx:          context.Background(),
				IsChatMode:   true,
			}

			switch tt.fromState {
			case domain.StateStreamingLLM:
				_ = sm.Transition(testCtx, domain.StateCheckingQueue)
				_ = sm.Transition(testCtx, domain.StateStreamingLLM)
			case domain.StatePostStream:
				_ = sm.Transition(testCtx, domain.StateCheckingQueue)
				_ = sm.Transition(testCtx, domain.StateStreamingLLM)
				_ = sm.Transition(testCtx, domain.StatePostStream)
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
		stateManager := &mockdomain.FakeStateManager{}
		sm := NewAgentStateMachine(stateManager)

		ctx := &domain.AgentContext{
			Conversation:   &[]sdk.Message{},
			MessageQueue:   &mockdomain.FakeMessageQueue{},
			Turns:          0,
			HasToolResults: false,
			Ctx:            context.Background(),
			IsChatMode:     true,
		}
		fakeQueue := ctx.MessageQueue.(*mockdomain.FakeMessageQueue)
		fakeQueue.IsEmptyReturns(true)

		_ = sm.Transition(ctx, domain.StateCheckingQueue)

		err := sm.Transition(ctx, domain.StateIdle)
		if err == nil {
			t.Error("expected transition to fail when canComplete guard fails (turns=0)")
		}

		ctx.Turns = 1
		err = sm.Transition(ctx, domain.StateIdle)
		if err != nil {
			t.Errorf("expected transition to succeed when canComplete guard passes, got: %v", err)
		}
	})

	t.Run("PostToolExecution respects maxTurnsReached guard", func(t *testing.T) {
		stateManager := &mockdomain.FakeStateManager{}
		sm := NewAgentStateMachine(stateManager)

		ctx := &domain.AgentContext{
			Conversation:   &[]sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}},
			MessageQueue:   &mockdomain.FakeMessageQueue{},
			ToolCalls:      make(map[string]*sdk.ChatCompletionMessageToolCall),
			Turns:          10,
			MaxTurns:       10,
			HasToolResults: false,
			Ctx:            context.Background(),
			IsChatMode:     true,
		}
		fakeQueue := ctx.MessageQueue.(*mockdomain.FakeMessageQueue)
		fakeQueue.IsEmptyReturns(true)

		_ = sm.Transition(ctx, domain.StateCheckingQueue)
		_ = sm.Transition(ctx, domain.StateStreamingLLM)
		_ = sm.Transition(ctx, domain.StatePostStream)

		ctx.ToolCalls["0"] = &sdk.ChatCompletionMessageToolCall{
			Id: "test",
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "test",
				Arguments: "{}",
			},
		}
		_ = sm.Transition(ctx, domain.StateEvaluatingTools)
		_ = sm.Transition(ctx, domain.StateExecutingTools)
		_ = sm.Transition(ctx, domain.StatePostToolExecution)

		if !sm.CanTransition(ctx, domain.StateCompleting) {
			t.Error("expected to be able to transition to Completing when max turns reached")
		}
	})
}

// TestStateReset tests that the state machine can be reset
func TestStateReset(t *testing.T) {
	stateManager := &mockdomain.FakeStateManager{}
	sm := NewAgentStateMachine(stateManager)

	ctx := &domain.AgentContext{
		Conversation: &[]sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}},
		MessageQueue: &mockdomain.FakeMessageQueue{},
		Ctx:          context.Background(),
	}

	_ = sm.Transition(ctx, domain.StateCheckingQueue)
	_ = sm.Transition(ctx, domain.StateStreamingLLM)

	sm.Reset()

	if sm.GetCurrentState() != domain.StateIdle {
		t.Errorf("expected state to be Idle after reset, got %s", sm.GetCurrentState())
	}
}

// TestGetValidTransitions tests that valid transitions are returned correctly
func TestGetValidTransitions(t *testing.T) {
	stateManager := &mockdomain.FakeStateManager{}
	sm := NewAgentStateMachine(stateManager)

	ctx := &domain.AgentContext{
		Conversation:   &[]sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("test")}},
		MessageQueue:   &mockdomain.FakeMessageQueue{},
		Turns:          0,
		MaxTurns:       10,
		HasToolResults: false,
		Ctx:            context.Background(),
	}

	fakeQueue := ctx.MessageQueue.(*mockdomain.FakeMessageQueue)
	fakeQueue.IsEmptyReturns(true)

	validStates := sm.GetValidTransitions(ctx)

	found := false
	for _, state := range validStates {
		if state == domain.StateCheckingQueue {
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
		setupCtx func(*domain.AgentContext)
		expected bool
	}{
		{
			name: "returns_false_when_turns_is_zero",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.Turns = 0
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*mockdomain.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.Assistant, Content: sdk.NewMessageContent("test")},
				}
			},
			expected: false,
		},
		{
			name: "returns_false_when_has_tool_results",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = true
				fakeQueue := ctx.MessageQueue.(*mockdomain.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.Assistant, Content: sdk.NewMessageContent("test")},
				}
			},
			expected: false,
		},
		{
			name: "returns_false_when_queue_not_empty",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*mockdomain.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(false)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.Assistant, Content: sdk.NewMessageContent("test")},
				}
			},
			expected: false,
		},
		{
			name: "returns_false_when_last_message_is_user",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*mockdomain.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.User, Content: sdk.NewMessageContent("test")},
				}
			},
			expected: false,
		},
		{
			name: "returns_true_when_all_conditions_met",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*mockdomain.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.Assistant, Content: sdk.NewMessageContent("done")},
				}
			},
			expected: true,
		},
		{
			name: "returns_false_when_conversation_empty",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = false
				fakeQueue := ctx.MessageQueue.(*mockdomain.FakeMessageQueue)
				fakeQueue.IsEmptyReturns(true)
				*ctx.Conversation = []sdk.Message{}
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateManager := &mockdomain.FakeStateManager{}
			sm := NewAgentStateMachine(stateManager)
			smImpl := sm.(*AgentStateMachineImpl)

			ctx := &domain.AgentContext{
				Conversation:   &[]sdk.Message{},
				MessageQueue:   &mockdomain.FakeMessageQueue{},
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
		setupCtx func(*domain.AgentContext)
		expected bool
	}{
		{
			name: "returns_false_when_no_approval_policy",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.ApprovalPolicy = nil
				ctx.ToolCalls = map[string]*sdk.ChatCompletionMessageToolCall{
					"0": {
						Id: "call-1",
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
			setupCtx: func(ctx *domain.AgentContext) {
				fakePolicy := &mockdomain.FakeApprovalPolicy{}
				fakePolicy.ShouldRequireApprovalReturns(true)
				ctx.ApprovalPolicy = fakePolicy
				ctx.ToolCalls = map[string]*sdk.ChatCompletionMessageToolCall{
					"0": {
						Id: "call-1",
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
			setupCtx: func(ctx *domain.AgentContext) {
				fakePolicy := &mockdomain.FakeApprovalPolicy{}
				fakePolicy.ShouldRequireApprovalReturns(false)
				ctx.ApprovalPolicy = fakePolicy
				ctx.ToolCalls = map[string]*sdk.ChatCompletionMessageToolCall{
					"0": {
						Id: "call-1",
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
			setupCtx: func(ctx *domain.AgentContext) {
				fakePolicy := &mockdomain.FakeApprovalPolicy{}
				ctx.ApprovalPolicy = fakePolicy
				ctx.ToolCalls = map[string]*sdk.ChatCompletionMessageToolCall{}
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateManager := &mockdomain.FakeStateManager{}
			sm := NewAgentStateMachine(stateManager)
			smImpl := sm.(*AgentStateMachineImpl)

			ctx := &domain.AgentContext{
				ToolCalls:  make(map[string]*sdk.ChatCompletionMessageToolCall),
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
			stateManager := &mockdomain.FakeStateManager{}
			sm := NewAgentStateMachine(stateManager)
			smImpl := sm.(*AgentStateMachineImpl)

			ctx := &domain.AgentContext{
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
