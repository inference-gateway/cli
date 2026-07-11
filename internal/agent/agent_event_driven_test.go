package agent

import (
	"context"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	mockdomain "github.com/inference-gateway/cli/tests/mocks/domain"
	sdk "github.com/inference-gateway/sdk"
)

// testMocks holds all the mocks needed for testing
type testMocks struct {
	stateMachine *mockdomain.FakeAgentStateMachine
	queue        *mockdomain.FakeMessageQueue
	repo         *mockdomain.FakeConversationRepository
	stateManager *services.StateManager
	approval     *mockdomain.FakeApprovalPolicy
}

// setupTestMocks creates all required mocks
func setupTestMocks() *testMocks {
	return &testMocks{
		stateMachine: &mockdomain.FakeAgentStateMachine{},
		queue:        &mockdomain.FakeMessageQueue{},
		repo:         &mockdomain.FakeConversationRepository{},
		stateManager: services.NewStateManager(false),
		approval:     &mockdomain.FakeApprovalPolicy{},
	}
}

// createTestContext creates a minimal agent context for testing
func createTestContext(mocks *testMocks) *domain.AgentContext {
	conversation := []sdk.Message{}
	return &domain.AgentContext{
		RequestID:        "test-request-id",
		Conversation:     &conversation,
		MessageQueue:     mocks.queue,
		ConversationRepo: mocks.repo,
		ToolCalls:        nil,
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

	agent := &EventDrivenAgent{
		service:          service,
		cfg:              config.DefaultConfig().GetAgentConfig(),
		stateMachine:     mocks.stateMachine,
		agentCtx:         ctx,
		eventPublisher:   eventPublisher,
		events:           make(chan domain.AgentEvent, 100),
		currentToolCalls: []*sdk.ChatCompletionMessageToolCall{},
		stateHandlers:    make(map[domain.AgentExecutionState]domain.StateHandler),
		req:              &domain.AgentRequest{},
		provider:         "openai",
		model:            "gpt-4",
	}

	agent.registerStateHandlers()

	return agent
}

func TestHandleIdleState(t *testing.T) {
	tests := []struct {
		name          string
		event         domain.AgentEvent
		setupMocks    func(*testMocks)
		verifyMocks   func(*testing.T, *testMocks)
		expectedState domain.AgentExecutionState
	}{
		{
			name:  "message_received_transitions_to_checking_queue",
			event: domain.MessageReceivedEvent{},
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

			handler := agent.stateHandlers[domain.StateIdle]
			_ = handler.Handle(tt.event)

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks)
			}
		})
	}
}

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

			_ = agent.stateHandlers[domain.StateCheckingQueue].Handle(domain.MessageReceivedEvent{})

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks, agent)
			}
		})
	}
}

func TestHandleStreamingState(t *testing.T) {
	tests := []struct {
		name        string
		event       domain.AgentEvent
		setupMocks  func(*testMocks)
		verifyMocks func(*testing.T, *testMocks, *EventDrivenAgent)
	}{
		{
			name: "stream_completed_no_tools",
			event: domain.StreamCompletedEvent{
				Message: sdk.Message{
					Role:    sdk.Assistant,
					Content: sdk.NewMessageContent("response"),
				},
				ToolCalls:          []*sdk.ChatCompletionMessageToolCall{},
				Reasoning:          "",
				Usage:              nil,
				IterationStartTime: time.Now(),
			},
			setupMocks: func(m *testMocks) {
				m.stateMachine.TransitionReturns(nil)
				m.queue.IsEmptyReturns(true)
			},
			verifyMocks: func(t *testing.T, m *testMocks, a *EventDrivenAgent) {
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
			event: domain.StreamCompletedEvent{
				Message: sdk.Message{
					Role:    sdk.Assistant,
					Content: sdk.NewMessageContent(""),
				},
				ToolCalls: []*sdk.ChatCompletionMessageToolCall{
					{
						ID: "call-1",
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

			handler := agent.stateHandlers[domain.StateStreamingLLM]
			_ = handler.Handle(tt.event)

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks, agent)
			}
		})
	}
}

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
				a.currentToolCalls = []*sdk.ChatCompletionMessageToolCall{}
				ctx.Turns = 1
			},
			setupMocks: func(m *testMocks) {
				m.queue.IsEmptyReturns(false)
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks) {
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
				a.currentToolCalls = []*sdk.ChatCompletionMessageToolCall{}
				ctx.Turns = 0
			},
			setupMocks: func(m *testMocks) {
				m.queue.IsEmptyReturns(true)
				m.stateMachine.TransitionReturns(nil)
			},
			verifyMocks: func(t *testing.T, m *testMocks) {
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

			_ = agent.stateHandlers[domain.StatePostStream].Handle(domain.MessageReceivedEvent{})

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks)
			}
		})
	}
}

func TestHandleEvaluatingToolsState(t *testing.T) {
	t.Run("tools_need_approval_transitions_correctly", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		agent.currentToolCalls = []*sdk.ChatCompletionMessageToolCall{
			{
				ID: "call-1",
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Write",
					Arguments: `{"file":"test.txt"}`,
				},
			},
		}

		mocks.approval.ShouldRequireApprovalReturns(true)
		mocks.stateMachine.TransitionReturns(nil)

		_ = agent.stateHandlers[domain.StateEvaluatingTools].Handle(domain.MessageReceivedEvent{})

		assert.Equal(t, 1, mocks.stateMachine.TransitionCallCount())
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateApprovingTools, toState)

		select {
		case event := <-agent.events:
			assert.IsType(t, domain.MessageReceivedEvent{}, event)
		default:
			t.Error("Expected event to be emitted to channel")
		}
	})

	t.Run("tools_no_approval_needed_transitions_correctly", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		agent.toolExecutor = func() {
			agent.wg.Done()
		}

		agent.currentToolCalls = []*sdk.ChatCompletionMessageToolCall{
			{
				ID: "call-1",
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "Read",
					Arguments: `{"file":"test.txt"}`,
				},
			},
		}

		mocks.approval.ShouldRequireApprovalReturns(false)
		mocks.stateMachine.TransitionReturns(nil)

		_ = agent.stateHandlers[domain.StateEvaluatingTools].Handle(domain.MessageReceivedEvent{})

		assert.Equal(t, 1, mocks.stateMachine.TransitionCallCount())
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateExecutingTools, toState)

		agent.wg.Wait()
	})
}

func TestHandleExecutingToolsState(t *testing.T) {
	t.Run("tools_completed_event_triggers_transition", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)

		event := domain.ToolsCompletedEvent{
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

		_ = agent.stateHandlers[domain.StateExecutingTools].Handle(event)

		assert.GreaterOrEqual(t, mocks.stateMachine.TransitionCallCount(), 1)
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StatePostToolExecution, toState)
	})

	t.Run("tools_completed_event_with_stop_transitions_to_stopped", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)

		event := domain.ToolsCompletedEvent{
			Results: []domain.ConversationEntry{},
			Stop:    true,
		}

		mocks.stateMachine.TransitionReturns(nil)

		err := agent.stateHandlers[domain.StateExecutingTools].Handle(event)
		assert.NoError(t, err)

		assert.Equal(t, 1, mocks.stateMachine.TransitionCallCount())
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateStopped, toState)

		select {
		case ev := <-agent.events:
			t.Errorf("expected no event on stop path, got %T", ev)
		default:
		}
	})
}

func TestHandlePostToolExecutionState(t *testing.T) {
	t.Run("transitions_to_checking_queue_when_queue_not_empty", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)

		ctx.Turns = 1
		ctx.MaxTurns = 10

		callCount := 0
		mocks.queue.IsEmptyCalls(func() bool {
			callCount++
			return callCount != 1
		})
		mocks.stateMachine.TransitionReturns(nil)

		_ = agent.stateHandlers[domain.StatePostToolExecution].Handle(domain.MessageReceivedEvent{})

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

		_ = agent.stateHandlers[domain.StatePostToolExecution].Handle(domain.MessageReceivedEvent{})

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

		_ = agent.stateHandlers[domain.StatePostToolExecution].Handle(domain.MessageReceivedEvent{})

		assert.GreaterOrEqual(t, mocks.stateMachine.TransitionCallCount(), 1)
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateCheckingQueue, toState)
	})
}

// TestHandlePostToolExecutionState_DrainsThenStopsWhenCancelled verifies the
// "drain then stop" semantics for Esc cancellation: when the session ctx is
// cancelled and the message queue still has entries, the post-tool-execution
// handler must drain the queue (so queued user input lands in history) and
// then transition to Completing rather than starting another LLM turn.
func TestHandlePostToolExecutionState_DrainsThenStopsWhenCancelled(t *testing.T) {
	mocks := setupTestMocks()
	ctx := createTestContext(mocks)
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx.Ctx = cancelledCtx
	agent := createTestAgent(mocks, ctx)

	ctx.Turns = 1
	ctx.MaxTurns = 10

	// Calls 1+2 (the entry-log and the outer `if !IsEmpty()`) report
	// queue-not-empty; calls 3+ (inside batchDrainQueue's drain loop)
	// report empty so the loop exits.
	callCount := 0
	mocks.queue.IsEmptyCalls(func() bool {
		callCount++
		return callCount > 2
	})
	mocks.stateMachine.TransitionReturns(nil)

	_ = agent.stateHandlers[domain.StatePostToolExecution].Handle(domain.MessageReceivedEvent{})

	assert.GreaterOrEqual(t, mocks.stateMachine.TransitionCallCount(), 1)
	_, toState := mocks.stateMachine.TransitionArgsForCall(0)
	assert.Equal(t, domain.StateCompleting, toState,
		"cancelled session must short-circuit to Completing after drain, not loop back to CheckingQueue")
}

// TestHandleCheckingQueueState_StopsAfterDrainWhenCancelled verifies that
// CheckingQueue also honours the drain-then-stop contract.
func TestHandleCheckingQueueState_StopsAfterDrainWhenCancelled(t *testing.T) {
	mocks := setupTestMocks()
	ctx := createTestContext(mocks)
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	ctx.Ctx = cancelledCtx
	agent := createTestAgent(mocks, ctx)

	ctx.Turns = 1
	ctx.HasToolResults = false

	mocks.queue.IsEmptyReturns(true)
	mocks.stateMachine.TransitionReturns(nil)

	_ = agent.stateHandlers[domain.StateCheckingQueue].Handle(domain.MessageReceivedEvent{})

	assert.GreaterOrEqual(t, mocks.stateMachine.TransitionCallCount(), 1)
	_, toState := mocks.stateMachine.TransitionArgsForCall(0)
	assert.Equal(t, domain.StateCompleting, toState,
		"cancelled session in CheckingQueue must transition to Completing, not StreamingLLM")
}

// TestProcessEvents_PriorityProbeFavoursCancellation verifies the double-select
// pattern: even when the events channel has pending work, a closed cancelChan
// takes priority and exits the loop without draining events. This is the core
// fix for the "Esc requires multiple presses" bug - a single Esc closes the
// channel, and the next loop iteration always sees it first.
func TestProcessEvents_PriorityProbeFavoursCancellation(t *testing.T) {
	mocks := setupTestMocks()
	agentCtx := createTestContext(mocks)
	agent := createTestAgent(mocks, agentCtx)
	agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

	cancelChan := make(chan struct{})
	agent.cancelChan = cancelChan

	for range 50 {
		agent.events <- domain.MessageReceivedEvent{}
	}

	close(cancelChan)

	mocks.stateMachine.TransitionReturns(nil)

	agent.wg.Add(1)
	done := make(chan struct{})
	go func() {
		agent.processEvents()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("processEvents did not exit within 2s - cancelChan priority probe broken")
	}

	assert.GreaterOrEqual(t, mocks.stateMachine.TransitionCallCount(), 1)
	foundCancelled := false
	for i := range mocks.stateMachine.TransitionCallCount() {
		_, toState := mocks.stateMachine.TransitionArgsForCall(i)
		if toState == domain.StateCancelled {
			foundCancelled = true
			break
		}
	}
	assert.True(t, foundCancelled, "expected a transition to StateCancelled")
}

// TestProcessEvents_PublishesCancelledFlag verifies that the ChatCompleteEvent
// emitted on the cancel path carries Cancelled=true so the chat UI can
// distinguish "User interrupted" from "Response complete".
func TestProcessEvents_PublishesCancelledFlag(t *testing.T) {
	mocks := setupTestMocks()
	agentCtx := createTestContext(mocks)
	agent := createTestAgent(mocks, agentCtx)
	agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

	chatEvents := make(chan domain.ChatEvent, 10)
	agent.eventPublisher = &eventPublisher{chatEvents: chatEvents}

	cancelChan := make(chan struct{})
	agent.cancelChan = cancelChan
	close(cancelChan)

	mocks.stateMachine.TransitionReturns(nil)

	agent.wg.Add(1)
	done := make(chan struct{})
	go func() {
		agent.processEvents()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("processEvents did not exit within 2s")
	}

	select {
	case ev := <-chatEvents:
		complete, ok := ev.(domain.ChatCompleteEvent)
		assert.True(t, ok, "expected a ChatCompleteEvent on the cancel path")
		assert.True(t, complete.Cancelled, "ChatCompleteEvent.Cancelled must be true on user cancellation")
	default:
		t.Fatal("expected a ChatCompleteEvent on the events channel")
	}
}

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

			_ = agent.stateHandlers[domain.StateCompleting].Handle(domain.CompletionRequestedEvent{})

			if tt.verifyMocks != nil {
				tt.verifyMocks(t, mocks)
			}
		})
	}
}

func TestHandleApprovingToolsState(t *testing.T) {
	t.Run("all_tools_processed_transitions_to_post_tool_execution", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		mocks.stateMachine.TransitionReturns(nil)

		_ = agent.stateHandlers[domain.StateApprovingTools].Handle(domain.AllToolsProcessedEvent{})

		assert.Equal(t, 1, mocks.stateMachine.TransitionCallCount())
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StatePostToolExecution, toState)

		select {
		case event := <-agent.events:
			_, ok := event.(domain.MessageReceivedEvent)
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

		toolCall1 := &sdk.ChatCompletionMessageToolCall{
			ID:   "call-1",
			Type: sdk.Function,
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      "test_tool",
				Arguments: "{}",
			},
		}
		agent.currentToolCalls = []*sdk.ChatCompletionMessageToolCall{
			toolCall1,
		}

		go func() {
			_ = agent.stateHandlers[domain.StateApprovingTools].Handle(domain.MessageReceivedEvent{})
		}()

		time.Sleep(10 * time.Millisecond)

		agent.mu.Lock()
		assert.Equal(t, 1, len(agent.toolsNeedingApproval))
		assert.Equal(t, 1, agent.currentToolIndex)
		assert.NotNil(t, agent.toolResults)
		agent.mu.Unlock()
	})

	t.Run("approval_failed_transitions_to_error", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		mocks.stateMachine.TransitionReturns(nil)

		_ = agent.stateHandlers[domain.StateApprovingTools].Handle(domain.ApprovalFailedEvent{Error: context.DeadlineExceeded})

		assert.Equal(t, 1, mocks.stateMachine.TransitionCallCount())
		_, toState := mocks.stateMachine.TransitionArgsForCall(0)
		assert.Equal(t, domain.StateError, toState)
	})

	t.Run("cancelled_ctx_short_circuits_to_all_tools_processed", func(t *testing.T) {
		mocks := setupTestMocks()
		ctx := createTestContext(mocks)
		cancelledCtx, cancel := context.WithCancel(context.Background())
		cancel()
		ctx.Ctx = cancelledCtx
		agent := createTestAgent(mocks, ctx)
		agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}

		agent.currentToolCalls = []*sdk.ChatCompletionMessageToolCall{
			{
				ID:   "call-1",
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "test_tool",
					Arguments: "{}",
				},
			},
			{
				ID:   "call-2",
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "test_tool",
					Arguments: "{}",
				},
			},
		}

		_ = agent.stateHandlers[domain.StateApprovingTools].Handle(domain.MessageReceivedEvent{})

		select {
		case event := <-agent.events:
			_, ok := event.(domain.AllToolsProcessedEvent)
			assert.True(t, ok, "cancelled ctx must emit AllToolsProcessedEvent without running approval prompts")
		case <-time.After(500 * time.Millisecond):
			t.Fatal("processNextTool did not fast-exit on cancelled ctx")
		}
	})
}

// TestStart_DoesNotRedundantlyTransitionToIdle verifies Start() issues no
// redundant Idle transition: the machine already starts in Idle, so the first
// transition must be Idle -> CheckingQueue from the seeded MessageReceivedEvent.
func TestStart_DoesNotRedundantlyTransitionToIdle(t *testing.T) {
	mocks := setupTestMocks()
	ctx := createTestContext(mocks)
	agent := createTestAgent(mocks, ctx)
	agent.req = &domain.AgentRequest{RequestID: "test-123", Model: "test-model"}
	mocks.stateMachine.TransitionReturns(nil)

	agent.Start()

	done := make(chan struct{})
	go func() { agent.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not settle after Start(); processEvents never reached Idle")
	}

	if got := mocks.stateMachine.TransitionCallCount(); got < 1 {
		t.Fatalf("expected Start() to drive at least one transition, got %d", got)
	}
	_, firstTarget := mocks.stateMachine.TransitionArgsForCall(0)
	assert.NotEqual(t, domain.StateIdle, firstTarget,
		"Start() must not issue a redundant Idle transition; the first should be "+
			"Idle -> CheckingQueue from the seeded MessageReceivedEvent")
}
