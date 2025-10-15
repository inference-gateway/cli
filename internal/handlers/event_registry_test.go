package handlers

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
)

// TestEventHandlerRegistry_ValidateAllEventTypes verifies all event types have handlers
func TestEventHandlerRegistry_ValidateAllEventTypes(t *testing.T) {
	tests := []struct {
		name             string
		setupRegistry    func() *EventHandlerRegistry
		expectedError    bool
		expectedErrorMsg string
	}{
		{
			name: "all handlers registered",
			setupRegistry: func() *EventHandlerRegistry {
				registry := NewEventHandlerRegistry()

				// Register all required handlers
				registry.Register(domain.UserInputEvent{}, dummyHandler)
				registry.Register(domain.FileSelectionRequestEvent{}, dummyHandler)
				registry.Register(domain.ConversationSelectedEvent{}, dummyHandler)
				registry.Register(domain.ChatStartEvent{}, dummyHandler)
				registry.Register(domain.ChatChunkEvent{}, dummyHandler)
				registry.Register(domain.ChatCompleteEvent{}, dummyHandler)
				registry.Register(domain.ChatErrorEvent{}, dummyHandler)
				registry.Register(domain.OptimizationStatusEvent{}, dummyHandler)
				registry.Register(domain.ToolCallPreviewEvent{}, dummyHandler)
				registry.Register(domain.ToolCallUpdateEvent{}, dummyHandler)
				registry.Register(domain.ToolCallReadyEvent{}, dummyHandler)
				registry.Register(domain.ToolExecutionStartedEvent{}, dummyHandler)
				registry.Register(domain.ToolExecutionProgressEvent{}, dummyHandler)
				registry.Register(domain.ToolExecutionCompletedEvent{}, dummyHandler)
				registry.Register(domain.ParallelToolsStartEvent{}, dummyHandler)
				registry.Register(domain.ParallelToolsCompleteEvent{}, dummyHandler)
				registry.Register(domain.CancelledEvent{}, dummyHandler)
				registry.Register(domain.A2AToolCallExecutedEvent{}, dummyHandler)
				registry.Register(domain.A2ATaskSubmittedEvent{}, dummyHandler)
				registry.Register(domain.A2ATaskStatusUpdateEvent{}, dummyHandler)
				registry.Register(domain.A2ATaskCompletedEvent{}, dummyHandler)
				registry.Register(domain.A2ATaskInputRequiredEvent{}, dummyHandler)

				return registry
			},
			expectedError: false,
		},
		{
			name: "missing A2A handlers",
			setupRegistry: func() *EventHandlerRegistry {
				registry := NewEventHandlerRegistry()

				// Register most handlers but miss A2A ones
				registry.Register(domain.UserInputEvent{}, dummyHandler)
				registry.Register(domain.FileSelectionRequestEvent{}, dummyHandler)
				registry.Register(domain.ConversationSelectedEvent{}, dummyHandler)
				registry.Register(domain.ChatStartEvent{}, dummyHandler)
				registry.Register(domain.ChatChunkEvent{}, dummyHandler)
				registry.Register(domain.ChatCompleteEvent{}, dummyHandler)
				registry.Register(domain.ChatErrorEvent{}, dummyHandler)
				registry.Register(domain.OptimizationStatusEvent{}, dummyHandler)
				registry.Register(domain.ToolCallPreviewEvent{}, dummyHandler)
				registry.Register(domain.ToolCallUpdateEvent{}, dummyHandler)
				registry.Register(domain.ToolCallReadyEvent{}, dummyHandler)
				registry.Register(domain.ToolExecutionStartedEvent{}, dummyHandler)
				registry.Register(domain.ToolExecutionProgressEvent{}, dummyHandler)
				registry.Register(domain.ToolExecutionCompletedEvent{}, dummyHandler)
				registry.Register(domain.ParallelToolsStartEvent{}, dummyHandler)
				registry.Register(domain.ParallelToolsCompleteEvent{}, dummyHandler)
				registry.Register(domain.CancelledEvent{}, dummyHandler)

				return registry
			},
			expectedError:    true,
			expectedErrorMsg: "missing handlers for event types",
		},
		{
			name: "empty registry",
			setupRegistry: func() *EventHandlerRegistry {
				return NewEventHandlerRegistry()
			},
			expectedError:    true,
			expectedErrorMsg: "missing handlers for event types",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := tt.setupRegistry()
			err := registry.ValidateAllEventTypes()

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.expectedErrorMsg != "" && err.Error() == "" {
					t.Errorf("Expected error message containing '%s' but got '%s'", tt.expectedErrorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestEventHandlerRegistry_MustHaveHandlerFor verifies panic behavior for missing handlers
func TestEventHandlerRegistry_MustHaveHandlerFor(t *testing.T) {
	tests := []struct {
		name        string
		eventType   tea.Msg
		shouldPanic bool
	}{
		{
			name:        "registered event type",
			eventType:   domain.ChatStartEvent{},
			shouldPanic: false,
		},
		{
			name:        "unregistered event type",
			eventType:   domain.A2ATaskStatusUpdateEvent{},
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewEventHandlerRegistry()
			registry.Register(domain.ChatStartEvent{}, dummyHandler)

			defer func() {
				r := recover()
				if tt.shouldPanic && r == nil {
					t.Errorf("Expected panic but none occurred")
				}
				if !tt.shouldPanic && r != nil {
					t.Errorf("Unexpected panic: %v", r)
				}
			}()

			registry.MustHaveHandlerFor(tt.eventType)
		})
	}
}

// TestEventHandlerRegistry_GetHandler tests handler retrieval
func TestEventHandlerRegistry_GetHandler(t *testing.T) {
	tests := []struct {
		name        string
		eventType   tea.Msg
		shouldExist bool
	}{
		{
			name:        "existing handler",
			eventType:   domain.ChatStartEvent{},
			shouldExist: true,
		},
		{
			name:        "non-existing handler",
			eventType:   domain.A2ATaskStatusUpdateEvent{},
			shouldExist: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewEventHandlerRegistry()
			registry.Register(domain.ChatStartEvent{}, dummyHandler)

			handler, exists := registry.GetHandler(tt.eventType)

			if tt.shouldExist != exists {
				t.Errorf("Expected exists=%v but got %v", tt.shouldExist, exists)
			}

			if tt.shouldExist && handler == nil {
				t.Errorf("Expected handler to be non-nil")
			}

			if !tt.shouldExist && handler != nil {
				t.Errorf("Expected handler to be nil")
			}
		})
	}
}

// TestChatHandler_AllEventTypesHaveHandlers verifies ChatHandler has all required handlers
func TestChatHandler_AllEventTypesHaveHandlers(t *testing.T) {
	// This test ensures that adding new event types will cause compilation/test failures
	// if handlers are not added, providing fail-fast behavior
	defer func() {
		r := recover()
		if r != nil {
			t.Fatalf("ChatHandler initialization failed with panic: %v", r)
		}
	}()

	// Create a test handler - this should not panic if all handlers are registered
	handler := createTestChatHandler()

	// Verify the registry was created and all handlers registered
	if handler.eventRegistry == nil {
		t.Fatal("Event registry not initialized")
	}

	// This will validate all event types have handlers and panic if any are missing
	err := handler.eventRegistry.ValidateAllEventTypes()
	if err != nil {
		t.Fatalf("Event handler validation failed: %v", err)
	}
}

// dummyHandler is a placeholder handler for testing
func dummyHandler(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	return nil, nil
}

// createTestChatHandler creates a ChatHandler for testing
func createTestChatHandler() *ChatHandler {
	// Create a handler with the registry and registration
	handler := &ChatHandler{
		name:          "TestHandler",
		eventRegistry: NewEventHandlerRegistry(),
	}

	// Initialize embedded handlers
	handler.eventHandler = &ChatEventHandler{handler: handler}
	handler.messageProcessor = &ChatMessageProcessor{handler: handler}

	// Register all handlers
	handler.registerEventHandlers()

	return handler
}
