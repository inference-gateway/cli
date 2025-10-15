package handlers

import (
	"fmt"
	"reflect"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
)

// EventHandlerFunc defines the signature for event handler functions
type EventHandlerFunc func(msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd)

// EventHandlerRegistry provides automatic registration and validation of event handlers
type EventHandlerRegistry struct {
	handlers map[reflect.Type]EventHandlerFunc
}

// NewEventHandlerRegistry creates a new event handler registry
func NewEventHandlerRegistry() *EventHandlerRegistry {
	return &EventHandlerRegistry{
		handlers: make(map[reflect.Type]EventHandlerFunc),
	}
}

// Register adds a handler for a specific event type
func (r *EventHandlerRegistry) Register(eventType tea.Msg, handler EventHandlerFunc) {
	r.handlers[reflect.TypeOf(eventType)] = handler
}

// MustHaveHandlerFor verifies that a handler exists for the given event type
// Panics if no handler is registered (fail-fast behavior)
func (r *EventHandlerRegistry) MustHaveHandlerFor(eventType tea.Msg) {
	if _, exists := r.handlers[reflect.TypeOf(eventType)]; !exists {
		panic(fmt.Sprintf("No handler registered for event type: %T", eventType))
	}
}

// ValidateAllEventTypes ensures all event types have registered handlers
func (r *EventHandlerRegistry) ValidateAllEventTypes() error {
	var missingHandlers []string

	// Define all event types that should have handlers
	eventTypes := []tea.Msg{
		domain.UserInputEvent{},
		domain.FileSelectionRequestEvent{},
		domain.ConversationSelectedEvent{},
		domain.ChatStartEvent{},
		domain.ChatChunkEvent{},
		domain.ChatCompleteEvent{},
		domain.ChatErrorEvent{},
		domain.OptimizationStatusEvent{},
		domain.ToolCallPreviewEvent{},
		domain.ToolCallUpdateEvent{},
		domain.ToolCallReadyEvent{},
		domain.ToolExecutionStartedEvent{},
		domain.ToolExecutionProgressEvent{},
		domain.ToolExecutionCompletedEvent{},
		domain.ParallelToolsStartEvent{},
		domain.ParallelToolsCompleteEvent{},
		domain.CancelledEvent{},
		domain.A2AToolCallExecutedEvent{},
		domain.A2ATaskSubmittedEvent{},
		domain.A2ATaskStatusUpdateEvent{},
		domain.A2ATaskCompletedEvent{},
		domain.A2ATaskInputRequiredEvent{},
	}

	for _, eventType := range eventTypes {
		if _, exists := r.handlers[reflect.TypeOf(eventType)]; !exists {
			missingHandlers = append(missingHandlers, fmt.Sprintf("%T", eventType))
		}
	}

	if len(missingHandlers) > 0 {
		return fmt.Errorf("missing handlers for event types: %v", missingHandlers)
	}

	return nil
}

// GetHandler returns the handler for a specific event type
func (r *EventHandlerRegistry) GetHandler(eventType tea.Msg) (EventHandlerFunc, bool) {
	handler, exists := r.handlers[reflect.TypeOf(eventType)]
	return handler, exists
}
