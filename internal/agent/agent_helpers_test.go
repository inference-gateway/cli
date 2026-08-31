package agent

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// TestAccumulateToolCalls tests tool call accumulation
func TestAccumulateToolCalls(t *testing.T) {
	agentService := &AgentServiceImpl{
		toolCallsMap: make(map[string]*sdk.ChatCompletionMessageToolCall),
	}

	callID := "call-1"
	deltas := []sdk.ChatCompletionMessageToolCallChunk{
		{Index: 0, ID: &callID, Function: &sdk.ChatCompletionMessageToolCallFunction{Name: "Read", Arguments: `{"file":`}},
		{Index: 0, Function: &sdk.ChatCompletionMessageToolCallFunction{Arguments: `"test.txt"}`}},
	}

	agentService.accumulateToolCalls(deltas)

	assert.Equal(t, 1, len(agentService.toolCallsMap))
	assert.Contains(t, agentService.toolCallsMap, "0")
	assert.Equal(t, "call-1", agentService.toolCallsMap["0"].ID)
	assert.Equal(t, "Read", agentService.toolCallsMap["0"].Function.Name)
	assert.Equal(t, `{"file":"test.txt"}`, agentService.toolCallsMap["0"].Function.Arguments)
}

// TestGetAccumulatedToolCalls tests retrieving accumulated tool calls
func TestGetAccumulatedToolCalls(t *testing.T) {
	agentService := &AgentServiceImpl{
		toolCallsMap: map[string]*sdk.ChatCompletionMessageToolCall{
			"0": {ID: "call-1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}},
			"1": {ID: "call-2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Write"}},
		},
	}

	result := agentService.getAccumulatedToolCalls()

	assert.Equal(t, 2, len(result))
	assert.Equal(t, "call-1", result[0].ID)
	assert.Equal(t, "Read", result[0].Function.Name)
	assert.Equal(t, "call-2", result[1].ID)
	assert.Equal(t, "Write", result[1].Function.Name)

	// Verify map was cleared
	assert.Empty(t, agentService.toolCallsMap)
}

// TestClearToolCallsMap tests clearing tool calls map
func TestClearToolCallsMap(t *testing.T) {
	agentService := &AgentServiceImpl{
		toolCallsMap: map[string]*sdk.ChatCompletionMessageToolCall{
			"0": {ID: "call-1"},
		},
	}

	agentService.clearToolCallsMap()

	assert.Empty(t, agentService.toolCallsMap)
	assert.NotNil(t, agentService.toolCallsMap)
}

// Reminder interval/trigger gating moved to config.RemindersDue (see
// config/reminders_test.go); injection/emission lives in
// agent_reminder_emission_test.go.

// Per-mode system prompt selection (getSystemPromptForMode) was removed for
// issue #1134: message[0] stays byte-stable across mode switches.
// That contract is pinned by
// TestAgentServiceImpl_BuildSystemPromptByteStableAcrossModeSwitch in
// agent_test.go; the per-mode instructions ride the on_mode_change reminder
// (agent_mode_change_reminder_test.go, config/reminders_mode_change_test.go).

// TestCheckToolResultsStatus tests checking tool results for rejection and plan content
func TestCheckToolResultsStatus(t *testing.T) {
	agentService := &AgentServiceImpl{}

	tests := []struct {
		name              string
		toolResults       []convdomain.ConversationEntry
		expectedRejection bool
		expectedPlan      string
		expectedID        string
	}{
		{
			name:              "no_results",
			toolResults:       []convdomain.ConversationEntry{},
			expectedRejection: false,
			expectedPlan:      "",
		},
		{
			name: "with_rejection",
			toolResults: []convdomain.ConversationEntry{
				{
					Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("rejected")},
					ToolExecution: &agentdomain.ToolExecutionResult{
						ToolName: "Write",
						Success:  false,
						Rejected: true,
					},
				},
			},
			expectedRejection: true,
			expectedPlan:      "",
		},
		{
			name: "without_rejection",
			toolResults: []convdomain.ConversationEntry{
				{
					Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("result")},
					ToolExecution: &agentdomain.ToolExecutionResult{
						ToolName: "Read",
						Success:  true,
						Rejected: false,
					},
				},
			},
			expectedRejection: false,
			expectedPlan:      "",
		},
		{
			name: "multiple_results_with_rejection",
			toolResults: []convdomain.ConversationEntry{
				{
					Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("result1")},
					ToolExecution: &agentdomain.ToolExecutionResult{
						ToolName: "Read",
						Success:  true,
						Rejected: false,
					},
				},
				{
					Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("rejected")},
					ToolExecution: &agentdomain.ToolExecutionResult{
						ToolName: "Write",
						Success:  false,
						Rejected: true,
					},
				},
			},
			expectedRejection: true,
			expectedPlan:      "",
		},
		{
			name: "with_plan_approval",
			toolResults: []convdomain.ConversationEntry{
				{
					Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("plan")},
					ToolExecution: &agentdomain.ToolExecutionResult{
						ToolName: "RequestPlanApproval",
						Success:  true,
						Data: map[string]any{
							"plan":    "# Plan\n- step 1",
							"plan_id": "2026-06-28-090000-plan",
						},
					},
				},
			},
			expectedRejection: false,
			expectedPlan:      "# Plan\n- step 1",
			expectedID:        "2026-06-28-090000-plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasRejection, planContent, planID := agentService.checkToolResultsStatus(tt.toolResults)
			assert.Equal(t, tt.expectedRejection, hasRejection)
			assert.Equal(t, tt.expectedPlan, planContent)
			assert.Equal(t, tt.expectedID, planID)
		})
	}
}

// TestExtractPlanID tests pulling the stored plan ID out of a
// RequestPlanApproval tool result, including defensive/degenerate cases.
func TestExtractPlanID(t *testing.T) {
	tests := []struct {
		name     string
		result   *agentdomain.ToolExecutionResult
		expected string
	}{
		{name: "nil_result", result: nil, expected: ""},
		{name: "nil_data", result: &agentdomain.ToolExecutionResult{}, expected: ""},
		{
			name:     "data_not_a_map",
			result:   &agentdomain.ToolExecutionResult{Data: "oops"},
			expected: "",
		},
		{
			name:     "missing_plan_id_key",
			result:   &agentdomain.ToolExecutionResult{Data: map[string]any{"plan": "x"}},
			expected: "",
		},
		{
			name:     "plan_id_present",
			result:   &agentdomain.ToolExecutionResult{Data: map[string]any{"plan_id": "2026-06-28-090000-p"}},
			expected: "2026-06-28-090000-p",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractPlanID(tt.result))
		})
	}
}

// TestAddToolResultsToConversation tests adding tool results to conversation
func TestAddToolResultsToConversation(t *testing.T) {
	agentService := &AgentServiceImpl{}

	call1 := "call-1"
	call2 := "call-2"

	toolResults := []convdomain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("result1"),
				ToolCallID: &call1,
			},
			ToolExecution: &agentdomain.ToolExecutionResult{
				ToolName: "Read",
				Success:  true,
			},
		},
		{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("result2"),
				ToolCallID: &call2,
			},
			ToolExecution: &agentdomain.ToolExecutionResult{
				ToolName: "Write",
				Success:  true,
			},
		},
	}

	conversation := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("initial")},
	}

	agentService.addToolResultsToConversation(toolResults, &conversation, "openai/gpt-4o")

	assert.Equal(t, 3, len(conversation))
	assert.Equal(t, sdk.Tool, conversation[1].Role)
	assert.NotNil(t, conversation[1].ToolCallID)
	assert.Equal(t, "call-1", *conversation[1].ToolCallID)
	assert.Equal(t, sdk.Tool, conversation[2].Role)
	assert.NotNil(t, conversation[2].ToolCallID)
	assert.Equal(t, "call-2", *conversation[2].ToolCallID)
}

// TestBatchDrainQueue tests draining queued messages into conversation
func TestBatchDrainQueue(t *testing.T) {
	tests := []struct {
		name               string
		setupQueue         func(*convmocks.FakeMessageQueue)
		expectedBatched    int
		verifyRepo         func(*testing.T, *convmocks.FakeConversationRepository)
		verifyConversation func(*testing.T, *[]sdk.Message)
	}{
		{
			name: "empty_queue_returns_zero",
			setupQueue: func(q *convmocks.FakeMessageQueue) {
				q.IsEmptyReturns(true)
			},
			expectedBatched: 0,
			verifyRepo: func(t *testing.T, repo *convmocks.FakeConversationRepository) {
				assert.Equal(t, 0, repo.AddMessageCallCount())
			},
			verifyConversation: func(t *testing.T, conv *[]sdk.Message) {
				assert.Equal(t, 0, len(*conv))
			},
		},
		{
			name: "queue_with_one_message",
			setupQueue: func(q *convmocks.FakeMessageQueue) {
				callCount := 0
				q.IsEmptyCalls(func() bool {
					callCount++
					return callCount > 1
				})
				q.DequeueReturns(&convdomain.QueuedMessage{
					Message:   sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("queued message")},
					RequestID: "req-1",
				})
			},
			expectedBatched: 1,
			verifyRepo: func(t *testing.T, repo *convmocks.FakeConversationRepository) {
				assert.Equal(t, 1, repo.AddMessageCallCount())
			},
			verifyConversation: func(t *testing.T, conv *[]sdk.Message) {
				assert.Equal(t, 1, len(*conv))
				content, _ := (*conv)[0].Content.AsMessageContent0()
				assert.Equal(t, "queued message", content)
			},
		},
		{
			name: "queue_with_multiple_messages",
			setupQueue: func(q *convmocks.FakeMessageQueue) {
				callCount := 0
				q.IsEmptyCalls(func() bool {
					callCount++
					return callCount > 3
				})

				dequeueCount := 0
				q.DequeueCalls(func() *convdomain.QueuedMessage {
					dequeueCount++
					if dequeueCount > 3 {
						return nil
					}
					return &convdomain.QueuedMessage{
						Message:   sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("message " + string(rune('0'+dequeueCount)))},
						RequestID: "req-" + string(rune('0'+dequeueCount)),
					}
				})
			},
			expectedBatched: 3,
			verifyRepo: func(t *testing.T, repo *convmocks.FakeConversationRepository) {
				assert.Equal(t, 3, repo.AddMessageCallCount())
			},
			verifyConversation: func(t *testing.T, conv *[]sdk.Message) {
				assert.Equal(t, 3, len(*conv))
			},
		},
		{
			name: "queue_preserves_message_order",
			setupQueue: func(q *convmocks.FakeMessageQueue) {
				callCount := 0
				q.IsEmptyCalls(func() bool {
					callCount++
					return callCount > 2
				})

				dequeueCount := 0
				q.DequeueCalls(func() *convdomain.QueuedMessage {
					dequeueCount++
					switch dequeueCount {
					case 1:
						return &convdomain.QueuedMessage{
							Message:   sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("first")},
							RequestID: "req-1",
						}
					case 2:
						return &convdomain.QueuedMessage{
							Message:   sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("second")},
							RequestID: "req-2",
						}
					default:
						return nil
					}
				})
			},
			expectedBatched: 2,
			verifyRepo: func(t *testing.T, repo *convmocks.FakeConversationRepository) {
				assert.Equal(t, 2, repo.AddMessageCallCount())
			},
			verifyConversation: func(t *testing.T, conv *[]sdk.Message) {
				assert.Equal(t, 2, len(*conv))
				content1, _ := (*conv)[0].Content.AsMessageContent0()
				content2, _ := (*conv)[1].Content.AsMessageContent0()
				assert.Equal(t, "first", content1)
				assert.Equal(t, "second", content2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeQueue := &convmocks.FakeMessageQueue{}
			fakeRepo := &convmocks.FakeConversationRepository{}

			if tt.setupQueue != nil {
				tt.setupQueue(fakeQueue)
			}

			agentService := &AgentServiceImpl{
				messageQueue:     fakeQueue,
				conversationRepo: fakeRepo,
			}

			conversation := &[]sdk.Message{}
			eventPublisher := &eventPublisher{
				chatEvents: make(chan agentdomain.ChatEvent, 10),
			}

			result := agentService.batchDrainQueue(conversation, eventPublisher)

			assert.Equal(t, tt.expectedBatched, result)

			if tt.verifyRepo != nil {
				tt.verifyRepo(t, fakeRepo)
			}

			if tt.verifyConversation != nil {
				tt.verifyConversation(t, conversation)
			}
		})
	}
}

// TestBatchDrainQueue_NilMessageQueue tests behavior with nil message queue
func TestBatchDrainQueue_NilMessageQueue(t *testing.T) {
	agentService := &AgentServiceImpl{
		messageQueue: nil,
	}

	conversation := &[]sdk.Message{}
	eventPublisher := &eventPublisher{
		chatEvents: make(chan agentdomain.ChatEvent, 10),
	}

	result := agentService.batchDrainQueue(conversation, eventPublisher)

	assert.Equal(t, 0, result)
	assert.Equal(t, 0, len(*conversation))
}
