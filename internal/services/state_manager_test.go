package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	adkclient "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	domain "github.com/inference-gateway/cli/internal/domain"
	mocks "github.com/inference-gateway/cli/tests/mocks/generated"
	assert "github.com/stretchr/testify/assert"
	zap "go.uber.org/zap"
)

// MockADKClient implements the A2AClient interface for testing
type MockADKClient struct {
	CancelTaskFunc func(ctx context.Context, params adk.TaskIdParams) (*adk.JSONRPCSuccessResponse, error)
}

func (m *MockADKClient) GetAgentCard(ctx context.Context) (*adk.AgentCard, error) {
	return nil, nil
}

func (m *MockADKClient) GetHealth(ctx context.Context) (*adkclient.HealthResponse, error) {
	return nil, nil
}

func (m *MockADKClient) SendTask(ctx context.Context, params adk.MessageSendParams) (*adk.JSONRPCSuccessResponse, error) {
	return nil, nil
}

func (m *MockADKClient) SendTaskStreaming(ctx context.Context, params adk.MessageSendParams) (<-chan adk.JSONRPCSuccessResponse, error) {
	return nil, nil
}

func (m *MockADKClient) GetTask(ctx context.Context, params adk.TaskQueryParams) (*adk.JSONRPCSuccessResponse, error) {
	return nil, nil
}

func (m *MockADKClient) ListTasks(ctx context.Context, params adk.TaskListParams) (*adk.JSONRPCSuccessResponse, error) {
	return nil, nil
}

func (m *MockADKClient) CancelTask(ctx context.Context, params adk.TaskIdParams) (*adk.JSONRPCSuccessResponse, error) {
	if m.CancelTaskFunc != nil {
		return m.CancelTaskFunc(ctx, params)
	}
	return &adk.JSONRPCSuccessResponse{}, nil
}

func (m *MockADKClient) SetTimeout(timeout time.Duration) {}

func (m *MockADKClient) SetHTTPClient(client *http.Client) {}

func (m *MockADKClient) GetBaseURL() string {
	return ""
}

func (m *MockADKClient) SetLogger(logger *zap.Logger) {}

func (m *MockADKClient) GetLogger() *zap.Logger {
	return nil
}

func (m *MockADKClient) GetArtifactHelper() *adkclient.ArtifactHelper {
	return nil
}

// Test helper to create a state manager with mock ADK client
func createTestStateManager(mockClient *MockADKClient) *StateManager {
	createADKClient := func(agentURL string) adkclient.A2AClient {
		if mockClient != nil {
			return mockClient
		}
		return adkclient.NewClient(agentURL)
	}
	return NewStateManager(false, createADKClient)
}

func TestNewStateManager(t *testing.T) {
	tests := []struct {
		name          string
		debugMode     bool
		expectDebug   bool
		expectHistory int
	}{
		{
			name:          "Creates state manager with debug mode disabled",
			debugMode:     false,
			expectDebug:   false,
			expectHistory: 100,
		},
		{
			name:          "Creates state manager with debug mode enabled",
			debugMode:     true,
			expectDebug:   true,
			expectHistory: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createADKClient := func(agentURL string) adkclient.A2AClient {
				return adkclient.NewClient(agentURL)
			}
			sm := NewStateManager(tt.debugMode, createADKClient)

			assert.NotNil(t, sm)
			assert.NotNil(t, sm.state)
			assert.Equal(t, tt.expectDebug, sm.debugMode)
			assert.Equal(t, tt.expectHistory, sm.maxHistorySize)
			assert.Equal(t, 0, len(sm.listeners))
			assert.NotNil(t, sm.createADKClient)
		})
	}
}

// nolint:funlen
func TestCancelBackgroundTask(t *testing.T) {
	tests := []struct {
		name                 string
		taskID               string
		setupMocks           func(*mocks.FakeToolService, *mocks.FakeTaskTracker) (*MockADKClient, *bool, *bool)
		expectError          bool
		expectErrorContains  string
		expectADKCalled      bool
		expectCancelFuncCall bool
		expectStopPolling    bool
		expectRemoveTask     bool
	}{
		{
			name:   "Task not found - empty task list",
			taskID: "non-existent-task",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) (*MockADKClient, *bool, *bool) {
				toolService.GetTaskTrackerReturns(taskTracker)
				taskTracker.GetAllPollingTasksReturns([]string{})
				return &MockADKClient{}, new(bool), new(bool)
			},
			expectError:         true,
			expectErrorContains: "task non-existent-task not found in background tasks",
		},
		{
			name:   "Task not found - nil tool service",
			taskID: "task-123",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) (*MockADKClient, *bool, *bool) {
				return &MockADKClient{}, new(bool), new(bool)
			},
			expectError:         true,
			expectErrorContains: "task task-123 not found in background tasks",
		},
		{
			name:   "Task not found - nil task tracker",
			taskID: "task-123",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) (*MockADKClient, *bool, *bool) {
				toolService.GetTaskTrackerReturns(nil)
				return &MockADKClient{}, new(bool), new(bool)
			},
			expectError:         true,
			expectErrorContains: "task task-123 not found in background tasks",
		},
		{
			name:   "Successful cancellation with all components",
			taskID: "task-123",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) (*MockADKClient, *bool, *bool) {
				adkCalled := new(bool)
				cancelFuncCalled := new(bool)

				mockClient := &MockADKClient{
					CancelTaskFunc: func(ctx context.Context, params adk.TaskIdParams) (*adk.JSONRPCSuccessResponse, error) {
						*adkCalled = true
						return &adk.JSONRPCSuccessResponse{}, nil
					},
				}

				pollingState := &domain.TaskPollingState{
					TaskID:    "task-123",
					ContextID: "context-456",
					AgentURL:  "http://localhost:8081",
					IsPolling: true,
					CancelFunc: func() {
						*cancelFuncCalled = true
					},
				}

				toolService.GetTaskTrackerReturns(taskTracker)
				taskTracker.GetAllPollingTasksReturns([]string{"task-123"})
				taskTracker.GetPollingStateReturns(pollingState)

				return mockClient, adkCalled, cancelFuncCalled
			},
			expectError:          false,
			expectADKCalled:      true,
			expectCancelFuncCall: true,
			expectStopPolling:    true,
			expectRemoveTask:     true,
		},
		{
			name:   "ADK cancel fails but cleanup still happens",
			taskID: "task-123",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) (*MockADKClient, *bool, *bool) {
				adkCalled := new(bool)
				cancelFuncCalled := new(bool)

				mockClient := &MockADKClient{
					CancelTaskFunc: func(ctx context.Context, params adk.TaskIdParams) (*adk.JSONRPCSuccessResponse, error) {
						*adkCalled = true
						return nil, errors.New("ADK cancel failed")
					},
				}

				pollingState := &domain.TaskPollingState{
					TaskID:    "task-123",
					AgentURL:  "http://localhost:8081",
					IsPolling: true,
					CancelFunc: func() {
						*cancelFuncCalled = true
					},
				}

				toolService.GetTaskTrackerReturns(taskTracker)
				taskTracker.GetAllPollingTasksReturns([]string{"task-123"})
				taskTracker.GetPollingStateReturns(pollingState)

				return mockClient, adkCalled, cancelFuncCalled
			},
			expectError:          false,
			expectADKCalled:      true,
			expectCancelFuncCall: true,
			expectStopPolling:    true,
			expectRemoveTask:     true,
		},
		{
			name:   "Nil cancel function still completes cleanup",
			taskID: "task-123",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) (*MockADKClient, *bool, *bool) {
				adkCalled := new(bool)

				mockClient := &MockADKClient{
					CancelTaskFunc: func(ctx context.Context, params adk.TaskIdParams) (*adk.JSONRPCSuccessResponse, error) {
						*adkCalled = true
						return &adk.JSONRPCSuccessResponse{}, nil
					},
				}

				pollingState := &domain.TaskPollingState{
					TaskID:     "task-123",
					AgentURL:   "http://localhost:8081",
					IsPolling:  true,
					CancelFunc: nil,
				}

				toolService.GetTaskTrackerReturns(taskTracker)
				taskTracker.GetAllPollingTasksReturns([]string{"task-123"})
				taskTracker.GetPollingStateReturns(pollingState)

				return mockClient, adkCalled, new(bool)
			},
			expectError:          false,
			expectADKCalled:      true,
			expectCancelFuncCall: false,
			expectStopPolling:    true,
			expectRemoveTask:     true,
		},
		{
			name:   "Finds correct task among multiple tasks",
			taskID: "task-2",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) (*MockADKClient, *bool, *bool) {
				adkCalled := new(bool)
				cancelFuncCalled := new(bool)

				mockClient := &MockADKClient{
					CancelTaskFunc: func(ctx context.Context, params adk.TaskIdParams) (*adk.JSONRPCSuccessResponse, error) {
						*adkCalled = true
						assert.Equal(t, "task-2", params.ID)
						return &adk.JSONRPCSuccessResponse{}, nil
					},
				}

				task1 := &domain.TaskPollingState{TaskID: "task-1", AgentURL: "http://localhost:8081"}
				task2 := &domain.TaskPollingState{
					TaskID:     "task-2",
					AgentURL:   "http://localhost:8082",
					CancelFunc: func() { *cancelFuncCalled = true },
				}
				task3 := &domain.TaskPollingState{TaskID: "task-3", AgentURL: "http://localhost:8083"}

				toolService.GetTaskTrackerReturns(taskTracker)
				taskTracker.GetAllPollingTasksReturns([]string{"task-1", "task-2", "task-3"})
				taskTracker.GetPollingStateStub = func(taskID string) *domain.TaskPollingState {
					switch taskID {
					case "task-1":
						return task1
					case "task-2":
						return task2
					case "task-3":
						return task3
					default:
						return nil
					}
				}

				return mockClient, adkCalled, cancelFuncCalled
			},
			expectError:          false,
			expectADKCalled:      true,
			expectCancelFuncCall: true,
			expectStopPolling:    true,
			expectRemoveTask:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockToolService := &mocks.FakeToolService{}
			mockTaskTracker := &mocks.FakeTaskTracker{}

			mockClient, adkCalled, cancelFuncCalled := tt.setupMocks(mockToolService, mockTaskTracker)
			sm := createTestStateManager(mockClient)

			var err error
			if tt.name == "Task not found - nil tool service" {
				err = sm.CancelBackgroundTask(tt.taskID, nil)
			} else {
				err = sm.CancelBackgroundTask(tt.taskID, mockToolService)
			}

			if tt.expectError {
				assert.Error(t, err)
				if tt.expectErrorContains != "" {
					assert.Contains(t, err.Error(), tt.expectErrorContains)
				}
			} else {
				assert.NoError(t, err)
			}

			if tt.expectADKCalled {
				assert.True(t, *adkCalled, "ADK CancelTask should have been called")
			}

			if tt.expectCancelFuncCall {
				assert.True(t, *cancelFuncCalled, "Cancel function should have been called")
			}

			if tt.expectStopPolling {
				assert.Equal(t, 1, mockTaskTracker.StopPollingCallCount())
				stopTaskID := mockTaskTracker.StopPollingArgsForCall(0)
				assert.Equal(t, tt.taskID, stopTaskID)
			}

			if tt.expectRemoveTask {
				assert.Equal(t, 1, mockTaskTracker.RemoveTaskCallCount())
				removeTaskID := mockTaskTracker.RemoveTaskArgsForCall(0)
				assert.Equal(t, tt.taskID, removeTaskID)
			}
		})
	}
}

func TestCancelBackgroundTask_ADKClientFactory(t *testing.T) {
	receivedAgentURL := ""
	createADKClient := func(agentURL string) adkclient.A2AClient {
		receivedAgentURL = agentURL
		return &MockADKClient{
			CancelTaskFunc: func(ctx context.Context, params adk.TaskIdParams) (*adk.JSONRPCSuccessResponse, error) {
				return &adk.JSONRPCSuccessResponse{}, nil
			},
		}
	}

	sm := NewStateManager(false, createADKClient)

	mockToolService := &mocks.FakeToolService{}
	mockTaskTracker := &mocks.FakeTaskTracker{}

	expectedAgentURL := "http://agent.example.com:9000"
	pollingState := &domain.TaskPollingState{
		TaskID:     "task-123",
		ContextID:  "context-456",
		AgentURL:   expectedAgentURL,
		IsPolling:  true,
		CancelFunc: func() {},
	}

	mockToolService.GetTaskTrackerReturns(mockTaskTracker)
	mockTaskTracker.GetAllPollingTasksReturns([]string{"task-123"})
	mockTaskTracker.GetPollingStateReturns(pollingState)

	err := sm.CancelBackgroundTask("task-123", mockToolService)

	assert.NoError(t, err)
	assert.Equal(t, expectedAgentURL, receivedAgentURL, "Factory should be called with correct agent URL")
}

func TestGetBackgroundTasks(t *testing.T) {
	tests := []struct {
		name          string
		setupMocks    func(*mocks.FakeToolService, *mocks.FakeTaskTracker)
		expectNil     bool
		expectedCount int
		expectedTasks []string
	}{
		{
			name: "Nil tool service returns empty slice",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) {
			},
			expectNil:     true,
			expectedCount: 0,
		},
		{
			name: "Nil task tracker returns empty slice",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) {
				toolService.GetTaskTrackerReturns(nil)
			},
			expectedCount: 0,
		},
		{
			name: "Returns all polling tasks",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) {
				task1 := &domain.TaskPollingState{
					TaskID:    "task-1",
					AgentURL:  "http://localhost:8081",
					IsPolling: true,
				}
				task2 := &domain.TaskPollingState{
					TaskID:    "task-2",
					AgentURL:  "http://localhost:8082",
					IsPolling: true,
				}

				toolService.GetTaskTrackerReturns(taskTracker)
				taskTracker.GetAllPollingTasksReturns([]string{"task-1", "task-2"})
				taskTracker.GetPollingStateStub = func(taskID string) *domain.TaskPollingState {
					switch taskID {
					case "task-1":
						return task1
					case "task-2":
						return task2
					default:
						return nil
					}
				}
			},
			expectedCount: 2,
			expectedTasks: []string{"task-1", "task-2"},
		},
		{
			name: "Skips nil polling states",
			setupMocks: func(toolService *mocks.FakeToolService, taskTracker *mocks.FakeTaskTracker) {
				task1 := &domain.TaskPollingState{
					TaskID:   "task-1",
					AgentURL: "http://localhost:8081",
				}

				toolService.GetTaskTrackerReturns(taskTracker)
				taskTracker.GetAllPollingTasksReturns([]string{"task-1", "task-2", "task-3"})
				taskTracker.GetPollingStateStub = func(taskID string) *domain.TaskPollingState {
					if taskID == "task-1" {
						return task1
					}
					return nil
				}
			},
			expectedCount: 1,
			expectedTasks: []string{"task-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := createTestStateManager(nil)
			mockToolService := &mocks.FakeToolService{}
			mockTaskTracker := &mocks.FakeTaskTracker{}

			tt.setupMocks(mockToolService, mockTaskTracker)

			var tasks []domain.TaskPollingState
			if tt.expectNil {
				tasks = sm.GetBackgroundTasks(nil)
			} else {
				tasks = sm.GetBackgroundTasks(mockToolService)
			}

			assert.Len(t, tasks, tt.expectedCount)

			if tt.expectedTasks != nil {
				for i, expectedTaskID := range tt.expectedTasks {
					assert.Equal(t, expectedTaskID, tasks[i].TaskID)
				}
			}
		})
	}
}

func TestStateManager_ViewTransition(t *testing.T) {
	tests := []struct {
		name        string
		transitions []domain.ViewState
	}{
		{
			name:        "Transition to Chat view",
			transitions: []domain.ViewState{domain.ViewStateChat},
		},
		{
			name:        "Transition to Model Selection view",
			transitions: []domain.ViewState{domain.ViewStateModelSelection},
		},
		{
			name: "Multiple transitions",
			transitions: []domain.ViewState{
				domain.ViewStateChat,
				domain.ViewStateModelSelection,
				domain.ViewStateChat,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := createTestStateManager(nil)

			for _, view := range tt.transitions {
				err := sm.TransitionToView(view)
				assert.NoError(t, err)
				assert.Equal(t, view, sm.GetCurrentView())
			}
		})
	}
}

func TestStateManager_IsAgentBusy(t *testing.T) {
	tests := []struct {
		name       string
		status     domain.ChatStatus
		expectBusy bool
	}{
		{"Starting is busy", domain.ChatStatusStarting, true},
		{"Thinking is busy", domain.ChatStatusThinking, true},
		{"Generating is busy", domain.ChatStatusGenerating, true},
		{"WaitingTools is busy", domain.ChatStatusWaitingTools, true},
		{"ReceivingTools is busy", domain.ChatStatusReceivingTools, true},
		{"Completed is not busy", domain.ChatStatusCompleted, false},
		{"Error is not busy", domain.ChatStatusError, false},
		{"Cancelled is not busy", domain.ChatStatusCancelled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := createTestStateManager(nil)

			eventChan := make(chan domain.ChatEvent)
			_ = sm.StartChatSession("req-123", "test-model", eventChan)

			if tt.status != domain.ChatStatusStarting {
				_ = sm.UpdateChatStatus(tt.status)
			}

			assert.Equal(t, tt.expectBusy, sm.IsAgentBusy())

			sm.EndChatSession()
		})
	}
}

func TestStateManager_Dimensions(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"Small dimensions", 100, 50},
		{"Large dimensions", 1920, 1080},
		{"Zero dimensions", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := createTestStateManager(nil)

			sm.SetDimensions(tt.width, tt.height)
			width, height := sm.GetDimensions()

			assert.Equal(t, tt.width, width)
			assert.Equal(t, tt.height, height)
		})
	}
}

func TestStateManager_DebugMode(t *testing.T) {
	tests := []struct {
		name          string
		initialMode   bool
		setMode       bool
		expectedFinal bool
	}{
		{"Start disabled, enable", false, true, true},
		{"Start enabled, disable", true, false, false},
		{"Start disabled, keep disabled", false, false, false},
		{"Start enabled, keep enabled", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createADKClient := func(agentURL string) adkclient.A2AClient {
				return adkclient.NewClient(agentURL)
			}

			sm := NewStateManager(tt.initialMode, createADKClient)
			assert.Equal(t, tt.initialMode, sm.IsDebugMode())

			sm.SetDebugMode(tt.setMode)
			assert.Equal(t, tt.expectedFinal, sm.IsDebugMode())
		})
	}
}

func TestStateManager_QueuedMessages(t *testing.T) {
	sm := createTestStateManager(nil)

	messages := sm.GetQueuedMessages()
	assert.Empty(t, messages)

	msg1 := domain.Message{Role: domain.RoleUser, Content: "Hello"}
	msg2 := domain.Message{Role: domain.RoleUser, Content: "World"}

	sm.AddQueuedMessage(msg1, "req-1")
	sm.AddQueuedMessage(msg2, "req-2")

	messages = sm.GetQueuedMessages()
	assert.Len(t, messages, 2)

	popped := sm.PopQueuedMessage()
	assert.NotNil(t, popped)
	assert.Equal(t, "Hello", popped.Message.Content)
	assert.Equal(t, "req-1", popped.RequestID)

	messages = sm.GetQueuedMessages()
	assert.Len(t, messages, 1)

	sm.ClearQueuedMessages()
	messages = sm.GetQueuedMessages()
	assert.Empty(t, messages)
}

func TestStateManager_FileSelection(t *testing.T) {
	sm := createTestStateManager(nil)

	assert.Nil(t, sm.GetFileSelectionState())

	files := []string{"file1.go", "file2.go", "file3.go"}
	sm.SetupFileSelection(files)

	state := sm.GetFileSelectionState()
	assert.NotNil(t, state)
	assert.Len(t, state.Files, 3)

	sm.UpdateFileSearchQuery("file1")
	state = sm.GetFileSelectionState()
	assert.Equal(t, "file1", state.SearchQuery)

	sm.SetFileSelectedIndex(1)
	state = sm.GetFileSelectionState()
	assert.Equal(t, 1, state.SelectedIndex)

	sm.ClearFileSelectionState()
	assert.Nil(t, sm.GetFileSelectionState())
}

func TestStateManager_ChatSessionLifecycle(t *testing.T) {
	sm := createTestStateManager(nil)

	assert.Nil(t, sm.GetChatSession())
	assert.False(t, sm.IsAgentBusy())

	eventChan := make(chan domain.ChatEvent)
	err := sm.StartChatSession("req-123", "test-model", eventChan)
	assert.NoError(t, err)

	session := sm.GetChatSession()
	assert.NotNil(t, session)
	assert.Equal(t, "req-123", session.RequestID)
	assert.Equal(t, "test-model", session.Model)

	err = sm.UpdateChatStatus(domain.ChatStatusGenerating)
	assert.NoError(t, err)
	assert.True(t, sm.IsAgentBusy())

	sm.EndChatSession()
	assert.Nil(t, sm.GetChatSession())
	assert.False(t, sm.IsAgentBusy())
}

func TestStateManager_StateHistory(t *testing.T) {
	sm := createTestStateManager(nil)

	initialHistory := sm.GetStateHistory()
	assert.Empty(t, initialHistory)

	sm.SetDimensions(100, 50)
	sm.SetDimensions(200, 100)

	history := sm.GetStateHistory()
	assert.Len(t, history, 2)
}

func TestStateManager_ExportStateHistory(t *testing.T) {
	sm := createTestStateManager(nil)

	sm.SetDimensions(100, 50)

	exported, err := sm.ExportStateHistory()
	assert.NoError(t, err)
	assert.NotEmpty(t, exported)
	assert.Contains(t, string(exported), "width")
}

func TestStateManager_ValidateState(t *testing.T) {
	sm := createTestStateManager(nil)

	errors := sm.ValidateState()
	assert.Empty(t, errors)

	eventChan := make(chan domain.ChatEvent)
	_ = sm.StartChatSession("req-123", "test-model", eventChan)

	errors = sm.ValidateState()
	assert.Empty(t, errors)
}

func TestStateManager_ConcurrentAccess(t *testing.T) {
	sm := createTestStateManager(nil)

	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			sm.SetDimensions(i, i*2)
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_, _ = sm.GetDimensions()
			_ = sm.GetCurrentView()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	<-done
	<-done

	assert.True(t, true)
}
