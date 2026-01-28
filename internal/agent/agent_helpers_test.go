package agent

import (
	"testing"

	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// TestAccumulateToolCalls tests tool call accumulation
func TestAccumulateToolCalls(t *testing.T) {
	agentService := &AgentServiceImpl{
		toolCallsMap: make(map[string]*sdk.ChatCompletionMessageToolCall),
	}

	deltas := []sdk.ChatCompletionMessageToolCallChunk{
		{Index: 0, ID: "call-1", Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Name: "Read", Arguments: `{"file":`}},
		{Index: 0, Function: struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		}{Arguments: `"test.txt"}`}},
	}

	agentService.accumulateToolCalls(deltas)

	assert.Equal(t, 1, len(agentService.toolCallsMap))
	assert.Contains(t, agentService.toolCallsMap, "0")
	assert.Equal(t, "call-1", agentService.toolCallsMap["0"].Id)
	assert.Equal(t, "Read", agentService.toolCallsMap["0"].Function.Name)
	assert.Equal(t, `{"file":"test.txt"}`, agentService.toolCallsMap["0"].Function.Arguments)
}

// TestGetAccumulatedToolCalls tests retrieving accumulated tool calls
func TestGetAccumulatedToolCalls(t *testing.T) {
	agentService := &AgentServiceImpl{
		toolCallsMap: map[string]*sdk.ChatCompletionMessageToolCall{
			"0": {Id: "call-1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}},
			"1": {Id: "call-2", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Write"}},
		},
	}

	result := agentService.getAccumulatedToolCalls()

	assert.Equal(t, 2, len(result))
	assert.Equal(t, "call-1", result[0].Id)
	assert.Equal(t, "Read", result[0].Function.Name)
	assert.Equal(t, "call-2", result[1].Id)
	assert.Equal(t, "Write", result[1].Function.Name)

	// Verify map was cleared
	assert.Empty(t, agentService.toolCallsMap)
}

// TestClearToolCallsMap tests clearing tool calls map
func TestClearToolCallsMap(t *testing.T) {
	agentService := &AgentServiceImpl{
		toolCallsMap: map[string]*sdk.ChatCompletionMessageToolCall{
			"0": {Id: "call-1"},
		},
	}

	agentService.clearToolCallsMap()

	assert.Empty(t, agentService.toolCallsMap)
	assert.NotNil(t, agentService.toolCallsMap)
}

// TestShouldInjectSystemReminder tests system reminder injection logic
func TestShouldInjectSystemReminder(t *testing.T) {
	tests := []struct {
		name     string
		turns    int
		interval int
		enabled  bool
		expected bool
	}{
		{"disabled", 4, 4, false, false},
		{"matches_interval", 4, 4, true, true},
		{"doesn't_match", 3, 4, true, false},
		{"turn_zero", 0, 4, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeConfig := &domainmocks.FakeConfigService{}
			fakeConfig.GetAgentConfigReturns(&config.AgentConfig{
				SystemReminders: config.SystemRemindersConfig{
					Enabled:  tt.enabled,
					Interval: tt.interval,
				},
			})

			agentService := &AgentServiceImpl{config: fakeConfig}
			result := agentService.shouldInjectSystemReminder(tt.turns)

			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetSystemPromptForMode tests system prompt selection based on mode
func TestGetSystemPromptForMode(t *testing.T) {
	tests := []struct {
		name         string
		mode         domain.AgentMode
		systemPrompt string
		planPrompt   string
		expected     string
	}{
		{"standard_mode", domain.AgentModeStandard, "standard prompt", "plan prompt", "standard prompt"},
		{"plan_mode", domain.AgentModePlan, "standard prompt", "plan prompt", "plan prompt"},
		{"auto_accept_mode", domain.AgentModeAutoAccept, "standard prompt", "plan prompt", "standard prompt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeConfig := &domainmocks.FakeConfigService{}
			fakeConfig.GetAgentConfigReturns(&config.AgentConfig{
				SystemPrompt:     tt.systemPrompt,
				SystemPromptPlan: tt.planPrompt,
			})

			fakeStateManager := &domainmocks.FakeStateManager{}
			fakeStateManager.GetAgentModeReturns(tt.mode)

			agentService := &AgentServiceImpl{
				config:       fakeConfig,
				stateManager: fakeStateManager,
			}

			result := agentService.getSystemPromptForMode()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestCheckToolResultsStatus tests checking tool results for rejection and plan content
func TestCheckToolResultsStatus(t *testing.T) {
	agentService := &AgentServiceImpl{}

	tests := []struct {
		name              string
		toolResults       []domain.ConversationEntry
		expectedRejection bool
		expectedPlan      string
	}{
		{
			name:              "no_results",
			toolResults:       []domain.ConversationEntry{},
			expectedRejection: false,
			expectedPlan:      "",
		},
		{
			name: "with_rejection",
			toolResults: []domain.ConversationEntry{
				{
					Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("rejected")},
					ToolExecution: &domain.ToolExecutionResult{
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
			toolResults: []domain.ConversationEntry{
				{
					Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("result")},
					ToolExecution: &domain.ToolExecutionResult{
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
			toolResults: []domain.ConversationEntry{
				{
					Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("result1")},
					ToolExecution: &domain.ToolExecutionResult{
						ToolName: "Read",
						Success:  true,
						Rejected: false,
					},
				},
				{
					Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("rejected")},
					ToolExecution: &domain.ToolExecutionResult{
						ToolName: "Write",
						Success:  false,
						Rejected: true,
					},
				},
			},
			expectedRejection: true,
			expectedPlan:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasRejection, planContent := agentService.checkToolResultsStatus(tt.toolResults)
			assert.Equal(t, tt.expectedRejection, hasRejection)
			assert.Equal(t, tt.expectedPlan, planContent)
		})
	}
}

// TestAddToolResultsToConversation tests adding tool results to conversation
func TestAddToolResultsToConversation(t *testing.T) {
	agentService := &AgentServiceImpl{}

	call1 := "call-1"
	call2 := "call-2"

	toolResults := []domain.ConversationEntry{
		{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("result1"),
				ToolCallId: &call1,
			},
			ToolExecution: &domain.ToolExecutionResult{
				ToolName: "Read",
				Success:  true,
			},
		},
		{
			Message: sdk.Message{
				Role:       sdk.Tool,
				Content:    sdk.NewMessageContent("result2"),
				ToolCallId: &call2,
			},
			ToolExecution: &domain.ToolExecutionResult{
				ToolName: "Write",
				Success:  true,
			},
		},
	}

	conversation := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("initial")},
	}

	agentService.addToolResultsToConversation(toolResults, &conversation)

	assert.Equal(t, 3, len(conversation))
	assert.Equal(t, sdk.Tool, conversation[1].Role)
	assert.NotNil(t, conversation[1].ToolCallId)
	assert.Equal(t, "call-1", *conversation[1].ToolCallId)
	assert.Equal(t, sdk.Tool, conversation[2].Role)
	assert.NotNil(t, conversation[2].ToolCallId)
	assert.Equal(t, "call-2", *conversation[2].ToolCallId)
}

// TestBatchDrainQueue tests draining queued messages into conversation
func TestBatchDrainQueue(t *testing.T) {
	tests := []struct {
		name               string
		setupQueue         func(*domainmocks.FakeMessageQueue)
		expectedBatched    int
		verifyRepo         func(*testing.T, *domainmocks.FakeConversationRepository)
		verifyConversation func(*testing.T, *[]sdk.Message)
	}{
		{
			name: "empty_queue_returns_zero",
			setupQueue: func(q *domainmocks.FakeMessageQueue) {
				q.IsEmptyReturns(true)
			},
			expectedBatched: 0,
			verifyRepo: func(t *testing.T, repo *domainmocks.FakeConversationRepository) {
				assert.Equal(t, 0, repo.AddMessageCallCount())
			},
			verifyConversation: func(t *testing.T, conv *[]sdk.Message) {
				assert.Equal(t, 0, len(*conv))
			},
		},
		{
			name: "queue_with_one_message",
			setupQueue: func(q *domainmocks.FakeMessageQueue) {
				callCount := 0
				q.IsEmptyCalls(func() bool {
					callCount++
					return callCount > 1
				})
				q.DequeueReturns(&domain.QueuedMessage{
					Message:   sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("queued message")},
					RequestID: "req-1",
				})
			},
			expectedBatched: 1,
			verifyRepo: func(t *testing.T, repo *domainmocks.FakeConversationRepository) {
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
			setupQueue: func(q *domainmocks.FakeMessageQueue) {
				callCount := 0
				q.IsEmptyCalls(func() bool {
					callCount++
					return callCount > 3
				})

				dequeueCount := 0
				q.DequeueCalls(func() *domain.QueuedMessage {
					dequeueCount++
					if dequeueCount > 3 {
						return nil
					}
					return &domain.QueuedMessage{
						Message:   sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("message " + string(rune('0'+dequeueCount)))},
						RequestID: "req-" + string(rune('0'+dequeueCount)),
					}
				})
			},
			expectedBatched: 3,
			verifyRepo: func(t *testing.T, repo *domainmocks.FakeConversationRepository) {
				assert.Equal(t, 3, repo.AddMessageCallCount())
			},
			verifyConversation: func(t *testing.T, conv *[]sdk.Message) {
				assert.Equal(t, 3, len(*conv))
			},
		},
		{
			name: "queue_preserves_message_order",
			setupQueue: func(q *domainmocks.FakeMessageQueue) {
				callCount := 0
				q.IsEmptyCalls(func() bool {
					callCount++
					return callCount > 2
				})

				dequeueCount := 0
				q.DequeueCalls(func() *domain.QueuedMessage {
					dequeueCount++
					switch dequeueCount {
					case 1:
						return &domain.QueuedMessage{
							Message:   sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("first")},
							RequestID: "req-1",
						}
					case 2:
						return &domain.QueuedMessage{
							Message:   sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("second")},
							RequestID: "req-2",
						}
					default:
						return nil
					}
				})
			},
			expectedBatched: 2,
			verifyRepo: func(t *testing.T, repo *domainmocks.FakeConversationRepository) {
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
			fakeQueue := &domainmocks.FakeMessageQueue{}
			fakeRepo := &domainmocks.FakeConversationRepository{}

			if tt.setupQueue != nil {
				tt.setupQueue(fakeQueue)
			}

			agentService := &AgentServiceImpl{
				messageQueue:     fakeQueue,
				conversationRepo: fakeRepo,
			}

			conversation := &[]sdk.Message{}
			eventPublisher := &eventPublisher{
				chatEvents: make(chan domain.ChatEvent, 10),
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
		chatEvents: make(chan domain.ChatEvent, 10),
	}

	result := agentService.batchDrainQueue(conversation, eventPublisher)

	assert.Equal(t, 0, result)
	assert.Equal(t, 0, len(*conversation))
}
