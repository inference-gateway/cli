package services

import (
	"testing"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	assert "github.com/stretchr/testify/assert"
)

// Test helper to create a state manager
func createTestStateManager() *StateManager {
	return NewStateManager(false)
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
			sm := NewStateManager(tt.debugMode)

			assert.NotNil(t, sm)
			assert.NotNil(t, sm.state)
			assert.Equal(t, tt.expectDebug, sm.debugMode)
			assert.Equal(t, tt.expectHistory, sm.maxHistorySize)
			assert.Equal(t, 0, len(sm.listeners))
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
			sm := createTestStateManager()

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
			sm := createTestStateManager()

			eventChan := make(chan domain.ChatEvent)
			_ = sm.StartChatSession("req-123", "test-model", eventChan)

			if tt.status != domain.ChatStatusStarting {
				if tt.status == domain.ChatStatusCompleted {
					_ = sm.UpdateChatStatus(domain.ChatStatusGenerating)
				}
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
			sm := createTestStateManager()

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
			sm := NewStateManager(tt.initialMode)
			assert.Equal(t, tt.initialMode, sm.IsDebugMode())

			sm.SetDebugMode(tt.setMode)
			assert.Equal(t, tt.expectedFinal, sm.IsDebugMode())
		})
	}
}

func TestStateManager_QueuedMessages(t *testing.T) {
	sm := createTestStateManager()

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
	sm := createTestStateManager()

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
	sm := createTestStateManager()

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
	sm := createTestStateManager()

	initialHistory := sm.GetStateHistory()
	assert.Empty(t, initialHistory)

	sm.SetDimensions(100, 50)
	sm.SetDimensions(200, 100)

	history := sm.GetStateHistory()
	assert.Len(t, history, 2)
}

func TestStateManager_ExportStateHistory(t *testing.T) {
	sm := createTestStateManager()

	sm.SetDimensions(100, 50)

	exported, err := sm.ExportStateHistory()
	assert.NoError(t, err)
	assert.NotEmpty(t, exported)
	assert.Contains(t, string(exported), "width")
}

func TestStateManager_ValidateState(t *testing.T) {
	sm := createTestStateManager()

	errors := sm.ValidateState()
	assert.Empty(t, errors)

	eventChan := make(chan domain.ChatEvent)
	_ = sm.StartChatSession("req-123", "test-model", eventChan)

	errors = sm.ValidateState()
	assert.Empty(t, errors)
}

func TestStateManager_ConcurrentAccess(t *testing.T) {
	sm := createTestStateManager()

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
