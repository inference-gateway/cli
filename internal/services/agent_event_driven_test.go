package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	domain "github.com/inference-gateway/cli/internal/domain"
	mockdomain "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
)

// testMocks holds all the mocks needed for testing
type testMocks struct {
	stateMachine *mockdomain.FakeAgentStateMachine
	queue        *mockdomain.FakeMessageQueue
	repo         *mockdomain.FakeConversationRepository
	stateManager *mockdomain.FakeStateManager
	approval     *mockdomain.FakeApprovalPolicy
}

// setupTestMocks creates all required mocks
func setupTestMocks() *testMocks {
	return &testMocks{
		stateMachine: &mockdomain.FakeAgentStateMachine{},
		queue:        &mockdomain.FakeMessageQueue{},
		repo:         &mockdomain.FakeConversationRepository{},
		stateManager: &mockdomain.FakeStateManager{},
		approval:     &mockdomain.FakeApprovalPolicy{},
	}
}

// createTestContext creates a minimal agent context for testing
func createTestContext(mocks *testMocks) *domain.AgentContext {
	conversation := []sdk.Message{}
	return &domain.AgentContext{
		Conversation:     &conversation,
		MessageQueue:     mocks.queue,
		ConversationRepo: mocks.repo,
		ToolCalls:        make(map[string]*sdk.ChatCompletionMessageToolCall),
		EventPublisher:   nil,
		Turns:            0,
		MaxTurns:         10,
		HasToolResults:   false,
		ApprovalPolicy:   mocks.approval,
		Ctx:              context.Background(),
		IsChatMode:       true,
	}
}

// createTestAgent creates an EventDrivenAgent with test mocks
func createTestAgent(mocks *testMocks, ctx *domain.AgentContext) *EventDrivenAgent {
	service := &AgentServiceImpl{
		messageQueue:     mocks.queue,
		conversationRepo: mocks.repo,
		stateManager:     mocks.stateManager,
		approvalPolicy:   mocks.approval,
		toolCallsMap:     make(map[string]*sdk.ChatCompletionMessageToolCall),
	}

	eventPublisher := &eventPublisher{
		chatEvents: make(chan domain.ChatEvent, 100),
	}

	return &EventDrivenAgent{
		service:          service,
		stateMachine:     mocks.stateMachine,
		agentCtx:         ctx,
		eventPublisher:   eventPublisher,
		events:           make(chan AgentEvent, 100),
		currentToolCalls: make(map[string]*sdk.ChatCompletionMessageToolCall),
	}
}

// TestHandleIdleState tests the Idle state handler
func TestHandleIdleState(t *testing.T) {
	tests := []struct {
		name          string
		event         AgentEvent
		setupMocks    func(*testMocks)
		verifyMocks   func(*testing.T, *testMocks)
		expectedState domain.AgentExecutionState
	}{
		{
			name:  "message_received_transitions_to_checking_queue",
			event: MessageReceivedEvent{},
			setupMocks: func(m *testMocks) {
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks) {
				assert.Equal(t, 1, m.stateMachine.TransitionCallCount())
				_, toState := m.stateMachine.TransitionArgsForCall(0)
				assert.Equal(t, domain.StateCheckingQueue, toState)
			},
			expectedState: domain.StateCheckingQueue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mocks := setupTestMocks()
			ctx := createTestContext(mocks)
			agent := createTestAgent(mocks, ctx)

			if tt.setupMocks != nil {
				tt.setupMocks(mocks)
			}

			agent.handleIdleState(tt.event)

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks)
			}
		})
	}
}

// TestHandleCheckingQueueState tests the CheckingQueue state handler
func TestHandleCheckingQueueState(t *testing.T) {
	tests := []struct {
		name        string
		setupCtx    func(*domain.AgentContext)
		setupMocks  func(*testMocks)
		verifyMocks func(*testing.T, *testMocks, *EventDrivenAgent)
	}{
		{
			name: "has_tool_results_transitions_to_streaming",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.HasToolResults = true
				ctx.Turns = 1
			},
			setupMocks: func(m *testMocks) {
				m.queue.IsEmptyReturns(true)
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks, a *EventDrivenAgent) {
				assert.Equal(t, 1, m.stateMachine.TransitionCallCount())
				_, toState := m.stateMachine.TransitionArgsForCall(0)
				assert.Equal(t, domain.StateStreamingLLM, toState)
			},
		},
		{
			name: "queue_empty_can_complete",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.Turns = 1
				ctx.HasToolResults = false
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.Assistant, Content: sdk.NewMessageContent("done")},
				}
			},
			setupMocks: func(m *testMocks) {
				m.queue.IsEmptyReturns(true)
				m.stateMachine.CanTransitionReturns(true)
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks, a *EventDrivenAgent) {
				assert.Equal(t, 1, m.stateMachine.CanTransitionCallCount())
				assert.Equal(t, 1, m.stateMachine.TransitionCallCount())
				_, toState := m.stateMachine.TransitionArgsForCall(0)
				assert.Equal(t, domain.StateCompleting, toState)
			},
		},
		{
			name: "queue_empty_cannot_complete",
			setupCtx: func(ctx *domain.AgentContext) {
				ctx.Turns = 0
				ctx.HasToolResults = false
				*ctx.Conversation = []sdk.Message{
					{Role: sdk.User, Content: sdk.NewMessageContent("test")},
				}
			},
			setupMocks: func(m *testMocks) {
				m.queue.IsEmptyReturns(true)
				m.stateMachine.CanTransitionReturns(false)
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks, a *EventDrivenAgent) {
				assert.Equal(t, 1, m.stateMachine.TransitionCallCount())
				_, toState := m.stateMachine.TransitionArgsForCall(0)
				assert.Equal(t, domain.StateStreamingLLM, toState)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mocks := setupTestMocks()
			ctx := createTestContext(mocks)
			agent := createTestAgent(mocks, ctx)

			if tt.setupCtx != nil {
				tt.setupCtx(ctx)
			}
			if tt.setupMocks != nil {
				tt.setupMocks(mocks)
			}

			agent.handleCheckingQueueState(MessageReceivedEvent{})

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks, agent)
			}
		})
	}
}

// TestHandleStreamingState tests the StreamingLLM state handler
func TestHandleStreamingState(t *testing.T) {
	tests := []struct {
		name        string
		event       AgentEvent
		setupMocks  func(*testMocks)
		verifyMocks func(*testing.T, *testMocks, *EventDrivenAgent)
	}{
		{
			name: "stream_completed_no_tools",
			event: StreamCompletedEvent{
				Message: sdk.Message{
					Role:    sdk.Assistant,
					Content: sdk.NewMessageContent("response"),
				},
				ToolCalls:          map[string]*sdk.ChatCompletionMessageToolCall{},
				Reasoning:          "",
				Usage:              nil,
				IterationStartTime: time.Now(),
			},
			setupMocks: func(m *testMocks) {
				m.stateMachine.TransitionReturns(nil)
				m.queue.IsEmptyReturns(true)
			},
			verifyMocks: func(t *testing.T, m *testMocks, a *EventDrivenAgent) {
				// handleStreamingState calls Transition to StatePostStream
				// Then it calls handlePostStreamState which also calls Transition
				// So we expect at least 1 transition, possibly 2
				assert.GreaterOrEqual(t, m.stateMachine.TransitionCallCount(), 1)
				_, toState := m.stateMachine.TransitionArgsForCall(0)
				assert.Equal(t, domain.StatePostStream, toState)

				content, _ := a.currentMessage.Content.AsMessageContent0()
				assert.Equal(t, "response", content)
				assert.Equal(t, 0, len(a.currentToolCalls))
			},
		},
		{
			name: "stream_completed_with_tools",
			event: StreamCompletedEvent{
				Message: sdk.Message{
					Role:    sdk.Assistant,
					Content: sdk.NewMessageContent(""),
				},
				ToolCalls: map[string]*sdk.ChatCompletionMessageToolCall{
					"0": {
						Id: "call-1",
						Function: sdk.ChatCompletionMessageToolCallFunction{
							Name:      "Read",
							Arguments: `{"file":"test.txt"}`,
						},
					},
				},
				Reasoning:          "thinking...",
				Usage:              nil,
				IterationStartTime: time.Now(),
			},
			setupMocks: func(m *testMocks) {
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks, a *EventDrivenAgent) {
				assert.Equal(t, 1, len(a.currentToolCalls))
				assert.Equal(t, "thinking...", a.currentReasoning)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mocks := setupTestMocks()
			ctx := createTestContext(mocks)
			agent := createTestAgent(mocks, ctx)
			agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

			if tt.setupMocks != nil {
				tt.setupMocks(mocks)
			}

			agent.handleStreamingState(tt.event)

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks, agent)
			}
		})
	}
}

// TestHandlePostStreamState tests the PostStream state handler
func TestHandlePostStreamState(t *testing.T) {
	tests := []struct {
		name        string
		setupAgent  func(*EventDrivenAgent, *domain.AgentContext)
		setupMocks  func(*testMocks)
		verifyMocks func(*testing.T, *testMocks)
	}{
		{
			name: "queue_not_empty",
			setupAgent: func(a *EventDrivenAgent, ctx *domain.AgentContext) {
				a.currentMessage = sdk.Message{
					Role:    sdk.Assistant,
					Content: sdk.NewMessageContent("test"),
				}
				a.currentToolCalls = map[string]*sdk.ChatCompletionMessageToolCall{}
				ctx.Turns = 1
			},
			setupMocks: func(m *testMocks) {
				m.queue.IsEmptyReturns(false)
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks) {
				assert.Equal(t, 1, m.repo.AddMessageCallCount())
				assert.GreaterOrEqual(t, m.stateMachine.TransitionCallCount(), 1)
				_, toState := m.stateMachine.TransitionArgsForCall(0)
				assert.Equal(t, domain.StateCheckingQueue, toState)
			},
		},
		{
			name: "no_tools_cannot_complete",
			setupAgent: func(a *EventDrivenAgent, ctx *domain.AgentContext) {
				a.currentMessage = sdk.Message{
					Role:    sdk.Assistant,
					Content: sdk.NewMessageContent("partial"),
				}
				a.currentToolCalls = map[string]*sdk.ChatCompletionMessageToolCall{}
				ctx.Turns = 0
			},
			setupMocks: func(m *testMocks) {
				m.queue.IsEmptyReturns(true)
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks) {
				assert.Equal(t, 1, m.repo.AddMessageCallCount())
				assert.GreaterOrEqual(t, m.stateMachine.TransitionCallCount(), 1)
				_, toState := m.stateMachine.TransitionArgsForCall(0)
				assert.Equal(t, domain.StateCheckingQueue, toState)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mocks := setupTestMocks()
			ctx := createTestContext(mocks)
			agent := createTestAgent(mocks, ctx)
			agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

			if tt.setupAgent != nil {
				tt.setupAgent(agent, ctx)
			}
			if tt.setupMocks != nil {
				tt.setupMocks(mocks)
			}

			agent.handlePostStreamState(MessageReceivedEvent{})

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks)
			}
		})
	}
}

// TestHandleEvaluatingToolsState tests the EvaluatingTools state handler
func TestHandleEvaluatingToolsState(t *testing.T) {
	t.Run("tools_need_approval_transitions_correctly", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		agent.currentToolCalls = map[string]*sdk.ChatCompletionMessageToolCall{
			"0": {
				Id: "call-1",
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Write",
					Arguments: `{"file":"test.txt"}`,
				},
			},
		}

		mocks.approval.ShouldRequireApprovalReturns(true)
		mocks.stateMachine.TransitionReturns(nil)

		agent.handleEvaluatingToolsState(MessageReceivedEvent{})

		// Should transition to ApprovingTools
		assert.Equal(t, 1, mocks.stateMachine.TransitionCallCount())
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateApprovingTools, toState)

		// Should emit event (not call handler directly)
		// Event should be in the channel
		select {
		case event := <-agent.events:
			assert.IsType(t, MessageReceivedEvent{}, event)
		default:
			t.Error("Expected event to be emitted to channel")
		}
	})

	t.Run("tools_no_approval_needed_transitions_correctly", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		// Override toolExecutor to prevent actual tool execution in test
		agent.toolExecutor = func() {
			agent.wg.Done() // Must call Done() to match Add() in startToolExecution
			// No-op in test - don't actually execute tools
		}

		agent.currentToolCalls = map[string]*sdk.ChatCompletionMessageToolCall{
			"0": {
				Id: "call-1",
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Read",
					Arguments: `{"file":"test.txt"}`,
				},
			},
		}

		mocks.approval.ShouldRequireApprovalReturns(false)
		mocks.stateMachine.TransitionReturns(nil)

		agent.handleEvaluatingToolsState(MessageReceivedEvent{})

		// Should transition to ExecutingTools
		assert.Equal(t, 1, mocks.stateMachine.TransitionCallCount())
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateExecutingTools, toState)

		// Wait for goroutine to complete
		agent.wg.Wait()
	})
}

// TestHandleExecutingToolsState tests the ExecutingTools state handler
// NOTE: Full testing requires integration tests. This verifies basic event handling.
func TestHandleExecutingToolsState(t *testing.T) {
	t.Run("tools_completed_event_triggers_transition", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)

		event := ToolsCompletedEvent{
			Results: []domain.ConversationEntry{
				{
					Message: sdk.Message{
						Role:    sdk.Tool,
						Content: sdk.NewMessageContent("result"),
					},
					ToolExecution: &domain.ToolExecutionResult{
						ToolName: "Read",
						Success:  true,
					},
				},
			},
		}

		mocks.stateMachine.TransitionReturns(nil)
		mocks.queue.IsEmptyReturns(true)
		mocks.stateMachine.CanTransitionReturns(false)

		agent.handleExecutingToolsState(event)

		// Should transition to PostToolExecution
		assert.GreaterOrEqual(t, mocks.stateMachine.TransitionCallCount(), 1)
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StatePostToolExecution, toState)
	})
}

// TestHandlePostToolExecutionState tests the PostToolExecution state handler
// This handler doesn't spawn goroutines, so it's safe to test
func TestHandlePostToolExecutionState(t *testing.T) {
	t.Run("transitions_to_checking_queue_when_queue_not_empty", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)

		ctx.Turns = 1
		ctx.MaxTurns = 10

		// Mock batchDrainQueue behavior
		callCount := 0
		mocks.queue.IsEmptyCalls(func() bool {
			callCount++
			if callCount == 1 {
				return false // First call: queue not empty
			}
			return true // Subsequent calls: queue empty
		})
		mocks.stateMachine.TransitionReturns(nil)

		agent.handlePostToolExecutionState(MessageReceivedEvent{})

		assert.GreaterOrEqual(t, mocks.stateMachine.TransitionCallCount(), 1)
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateCheckingQueue, toState)
	})

	t.Run("transitions_to_completing_when_can_complete", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)

		ctx.Turns = 5
		ctx.MaxTurns = 10

		mocks.queue.IsEmptyReturns(true)
		mocks.stateMachine.CanTransitionReturns(true)
		mocks.stateMachine.TransitionReturns(nil)

		agent.handlePostToolExecutionState(MessageReceivedEvent{})

		assert.GreaterOrEqual(t, mocks.stateMachine.TransitionCallCount(), 1)
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateCompleting, toState)
	})

	t.Run("transitions_to_checking_queue_to_continue", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)

		ctx.Turns = 3
		ctx.MaxTurns = 10

		mocks.queue.IsEmptyReturns(true)
		mocks.stateMachine.CanTransitionReturns(false)
		mocks.stateMachine.TransitionReturns(nil)

		agent.handlePostToolExecutionState(MessageReceivedEvent{})

		assert.GreaterOrEqual(t, mocks.stateMachine.TransitionCallCount(), 1)
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateCheckingQueue, toState)
	})
}

// TestHandleCompletingState tests the Completing state handler
func TestHandleCompletingState(t *testing.T) {
	tests := []struct {
		name        string
		setupMocks  func(*testMocks)
		verifyMocks func(*testing.T, *testMocks)
	}{
		{
			name: "no_queued_messages_completes",
			setupMocks: func(m *testMocks) {
				m.queue.IsEmptyReturns(true)
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks) {
				assert.Equal(t, 1, m.stateMachine.TransitionCallCount())
				_, toState := m.stateMachine.TransitionArgsForCall(0)
				assert.Equal(t, domain.StateIdle, toState)
			},
		},
		{
			name: "messages_queued_after_completion",
			setupMocks: func(m *testMocks) {
				m.queue.IsEmptyReturns(false)
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks) {
				assert.Equal(t, 1, m.stateMachine.TransitionCallCount())
				_, toState := m.stateMachine.TransitionArgsForCall(0)
				assert.Equal(t, domain.StateCheckingQueue, toState)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mocks := setupTestMocks()
			ctx := createTestContext(mocks)
			agent := createTestAgent(mocks, ctx)
			agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

			if tt.setupMocks != nil {
				tt.setupMocks(mocks)
			}

			agent.handleCompletingState(CompletionRequestedEvent{})

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks)
			}
		})
	}
}

// TestHandleApprovingToolsState tests the ApprovingTools state handler
func TestHandleApprovingToolsState(t *testing.T) {
	t.Run("all_tools_processed_transitions_to_post_tool_execution", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		mocks.stateMachine.TransitionReturns(nil)

		agent.handleApprovingToolsState(AllToolsProcessedEvent{})

		assert.Equal(t, 1, mocks.stateMachine.TransitionCallCount())
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StatePostToolExecution, toState)

		// Should emit MessageReceivedEvent
		select {
		case event := <-agent.events:
			_, ok := event.(MessageReceivedEvent)
			assert.True(t, ok, "Should emit MessageReceivedEvent")
		case <-time.After(100 * time.Millisecond):
			t.Error("Expected MessageReceivedEvent")
		}
	})

	t.Run("message_received_initializes_tool_queue", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		// Set up some tool calls
		toolCall1 := &sdk.ChatCompletionMessageToolCall{
			Id:   "call-1",
			Type: sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "test_tool",
				Arguments: "{}",
			},
		}
		agent.currentToolCalls = map[string]*sdk.ChatCompletionMessageToolCall{
			"0": toolCall1,
		}

		// Process the event - this will spawn processNextTool in background
		// We'll just verify the initialization happened correctly
		go agent.handleApprovingToolsState(MessageReceivedEvent{})

		// Give it a moment to initialize
		time.Sleep(10 * time.Millisecond)

		// Tool queue should be initialized
		agent.mu.Lock()
		assert.Equal(t, 1, len(agent.toolsNeedingApproval))
		assert.Equal(t, 1, agent.currentToolIndex) // Will be 1 since processNextTool increments it
		assert.NotNil(t, agent.toolResults)
		agent.mu.Unlock()

		// Note: We don't wait for processNextTool to complete as it will block on approval
	})

	t.Run("approval_failed_transitions_to_error", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		mocks.stateMachine.TransitionReturns(nil)

		agent.handleApprovingToolsState(ApprovalFailedEvent{Error: context.DeadlineExceeded})

		assert.Equal(t, 1, mocks.stateMachine.TransitionCallCount())
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateError, toState)
	})
}
