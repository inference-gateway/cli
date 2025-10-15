package handlers

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
)

type MockHandler struct{}

func (m *MockHandler) HandleUserInputEvent(msg domain.UserInputEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleFileSelectionRequestEvent(msg domain.FileSelectionRequestEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleConversationSelectedEvent(msg domain.ConversationSelectedEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleChatStartEvent(msg domain.ChatStartEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleChatChunkEvent(msg domain.ChatChunkEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleChatCompleteEvent(msg domain.ChatCompleteEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleChatErrorEvent(msg domain.ChatErrorEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleOptimizationStatusEvent(msg domain.OptimizationStatusEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleToolCallPreviewEvent(msg domain.ToolCallPreviewEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleToolCallUpdateEvent(msg domain.ToolCallUpdateEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleToolCallReadyEvent(msg domain.ToolCallReadyEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleToolExecutionStartedEvent(msg domain.ToolExecutionStartedEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleToolExecutionProgressEvent(msg domain.ToolExecutionProgressEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleToolExecutionCompletedEvent(msg domain.ToolExecutionCompletedEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleParallelToolsStartEvent(msg domain.ParallelToolsStartEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleParallelToolsCompleteEvent(msg domain.ParallelToolsCompleteEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleCancelledEvent(msg domain.CancelledEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleA2AToolCallExecutedEvent(msg domain.A2AToolCallExecutedEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleA2ATaskSubmittedEvent(msg domain.A2ATaskSubmittedEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleA2ATaskStatusUpdateEvent(msg domain.A2ATaskStatusUpdateEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleA2ATaskCompletedEvent(msg domain.A2ATaskCompletedEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func (m *MockHandler) HandleA2ATaskInputRequiredEvent(msg domain.A2ATaskInputRequiredEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

type IncompleteHandler struct{}

func (i *IncompleteHandler) HandleUserInputEvent(msg domain.UserInputEvent, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

func TestEventRegistry_AutoRegistration(t *testing.T) {
	tests := []struct {
		name          string
		handler       interface{}
		shouldPanic   bool
		expectedPanic string
	}{
		{
			name:        "complete handler",
			handler:     &MockHandler{},
			shouldPanic: false,
		},
		{
			name:          "incomplete handler",
			handler:       &IncompleteHandler{},
			shouldPanic:   true,
			expectedPanic: "Missing handler method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if tt.shouldPanic && r == nil {
					t.Errorf("Expected panic but none occurred")
				}
				if !tt.shouldPanic && r != nil {
					t.Errorf("Unexpected panic: %v", r)
				}
				if tt.shouldPanic && r != nil && tt.expectedPanic != "" {
					panicMsg := r.(string)
					if len(panicMsg) < len(tt.expectedPanic) || panicMsg[:len(tt.expectedPanic)] != tt.expectedPanic {
						t.Errorf("Expected panic message to start with '%s' but got '%s'", tt.expectedPanic, panicMsg)
					}
				}
			}()

			NewEventRegistry(tt.handler)
		})
	}
}

func TestEventRegistry_Handle(t *testing.T) {
	tests := []struct {
		name      string
		event     tea.Msg
		wantPanic bool
	}{
		{
			name:      "valid event",
			event:     domain.UserInputEvent{},
			wantPanic: false,
		},
		{
			name:      "valid A2A event",
			event:     domain.A2ATaskStatusUpdateEvent{},
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewEventRegistry(&MockHandler{})

			defer func() {
				r := recover()
				if tt.wantPanic && r == nil {
					t.Errorf("Expected panic but none occurred")
				}
				if !tt.wantPanic && r != nil {
					t.Errorf("Unexpected panic: %v", r)
				}
			}()

			model, cmd := registry.Handle(&MockHandler{}, tt.event, nil)

			if !tt.wantPanic {
				if model != nil {
					t.Errorf("Expected nil model but got %v", model)
				}
				if cmd != nil {
					t.Errorf("Expected nil cmd but got %v", cmd)
				}
			}
		})
	}
}
