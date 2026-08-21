package services

import (
	"testing"
	"time"

	ui "github.com/inference-gateway/cli/internal/ui"

	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Test helper to create a state manager
func createTestStateManager() *StateManager {
	return NewStateManager(false)
}

func TestNewStateManager(t *testing.T) {
	tests := []struct {
		name        string
		debugMode   bool
		expectDebug bool
	}{
		{
			name:        "Creates state manager with debug mode disabled",
			debugMode:   false,
			expectDebug: false,
		},
		{
			name:        "Creates state manager with debug mode enabled",
			debugMode:   true,
			expectDebug: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewStateManager(tt.debugMode)

			assert.NotNil(t, sm)
			assert.NotNil(t, sm.state)
			assert.Equal(t, tt.expectDebug, sm.debugMode)
		})
	}
}

func TestStateManager_ViewTransition(t *testing.T) {
	tests := []struct {
		name        string
		transitions []ui.ViewState
	}{
		{
			name:        "Transition to Chat view",
			transitions: []ui.ViewState{ui.ViewStateChat},
		},
		{
			name:        "Transition to Model Selection view",
			transitions: []ui.ViewState{ui.ViewStateModelSelection},
		},
		{
			name: "Multiple transitions",
			transitions: []ui.ViewState{
				ui.ViewStateChat,
				ui.ViewStateModelSelection,
				ui.ViewStateChat,
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
		status     agentdomain.ChatStatus
		expectBusy bool
	}{
		{"Starting is busy", agentdomain.ChatStatusStarting, true},
		{"Thinking is busy", agentdomain.ChatStatusThinking, true},
		{"Generating is busy", agentdomain.ChatStatusGenerating, true},
		{"WaitingTools is busy", agentdomain.ChatStatusWaitingTools, true},
		{"ReceivingTools is busy", agentdomain.ChatStatusReceivingTools, true},
		{"Completed is not busy", agentdomain.ChatStatusCompleted, false},
		{"Error is not busy", agentdomain.ChatStatusError, false},
		{"Cancelled is not busy", agentdomain.ChatStatusCancelled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := createTestStateManager()

			eventChan := make(chan agentdomain.ChatEvent)
			_ = sm.StartChatSession("req-123", "test-model", eventChan)

			if tt.status != agentdomain.ChatStatusStarting {
				if tt.status == agentdomain.ChatStatusCompleted {
					_ = sm.UpdateChatStatus(agentdomain.ChatStatusGenerating)
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

	msg1 := sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("Hello")}
	msg2 := sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("World")}

	sm.AddQueuedMessage(msg1, "req-1")
	sm.AddQueuedMessage(msg2, "req-2")

	messages = sm.GetQueuedMessages()
	assert.Len(t, messages, 2)

	popped := sm.PopQueuedMessage()
	assert.NotNil(t, popped)
	poppedContent, _ := popped.Message.Content.AsMessageContent0()
	assert.Equal(t, "Hello", poppedContent)
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

	eventChan := make(chan agentdomain.ChatEvent)
	err := sm.StartChatSession("req-123", "test-model", eventChan)
	assert.NoError(t, err)

	session := sm.GetChatSession()
	assert.NotNil(t, session)
	assert.Equal(t, "req-123", session.RequestID)
	assert.Equal(t, "test-model", session.Model)

	err = sm.UpdateChatStatus(agentdomain.ChatStatusGenerating)
	assert.NoError(t, err)
	assert.True(t, sm.IsAgentBusy())

	sm.EndChatSession()
	assert.Nil(t, sm.GetChatSession())
	assert.False(t, sm.IsAgentBusy())
}

func TestStateManager_RetryStatus(t *testing.T) {
	sm := createTestStateManager()

	assert.Nil(t, sm.GetRetryStatus(), "no session means no retry status")

	err := sm.StartChatSession("req-123", "test-model", make(chan agentdomain.ChatEvent))
	assert.NoError(t, err)
	assert.Nil(t, sm.GetRetryStatus(), "fresh session is not stalled")

	sm.SetRetryStatus(&agentdomain.RetryStatus{Attempt: 2, MaxAttempts: 5})
	status := sm.GetRetryStatus()
	assert.NotNil(t, status)
	assert.Equal(t, 2, status.Attempt)

	sm.TouchChatActivity()
	assert.Nil(t, sm.GetRetryStatus(), "a chunk clears the retry status")

	sm.SetRetryStatus(&agentdomain.RetryStatus{Attempt: 5, MaxAttempts: 5})
	assert.NoError(t, sm.UpdateChatStatus(agentdomain.ChatStatusGenerating))
	assert.NoError(t, sm.UpdateChatStatus(agentdomain.ChatStatusError))
	assert.Nil(t, sm.GetRetryStatus(), "a terminal session never reports a retry status")
}

func TestStateManager_StallDetection(t *testing.T) {
	sm := createTestStateManager()
	sm.SetStallThreshold(10 * time.Millisecond)

	err := sm.StartChatSession("req-123", "test-model", make(chan agentdomain.ChatEvent))
	assert.NoError(t, err)

	assert.Nil(t, sm.GetRetryStatus(), "not stalled before the threshold elapses")

	time.Sleep(20 * time.Millisecond)
	status := sm.GetRetryStatus()
	assert.NotNil(t, status, "no chunks past the threshold reads as stalled")
	assert.Zero(t, status.Attempt, "synthesized stall status has no attempt count")

	sm.TouchChatActivity()
	assert.Nil(t, sm.GetRetryStatus(), "a chunk resets the stall clock")

	time.Sleep(20 * time.Millisecond)
	assert.NotNil(t, sm.GetRetryStatus(), "silence after the last chunk stalls again")

	assert.NoError(t, sm.UpdateChatStatus(agentdomain.ChatStatusGenerating))
	assert.NoError(t, sm.UpdateChatStatus(agentdomain.ChatStatusReceivingTools))
	assert.NoError(t, sm.UpdateChatStatus(agentdomain.ChatStatusWaitingTools))
	time.Sleep(20 * time.Millisecond)
	assert.Nil(t, sm.GetRetryStatus(), "local tool execution is not a stalled connection")

	sm.SetStallThreshold(0)
	assert.NoError(t, sm.UpdateChatStatus(agentdomain.ChatStatusStarting))
	time.Sleep(20 * time.Millisecond)
	assert.Nil(t, sm.GetRetryStatus(), "zero threshold disables stall detection")
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
