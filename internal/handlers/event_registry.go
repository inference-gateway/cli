package handlers

import (
	"fmt"
	"reflect"

	tea "github.com/charmbracelet/bubbletea"
	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
)

type EventRegistry struct {
	handlers map[reflect.Type]reflect.Method
}

func NewEventRegistry(handler interface{}) *EventRegistry {
	registry := &EventRegistry{
		handlers: make(map[reflect.Type]reflect.Method),
	}
	registry.autoRegisterHandlers(handler)
	return registry
}

func (r *EventRegistry) autoRegisterHandlers(handler interface{}) {
	handlerType := reflect.TypeOf(handler)

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
		methodName := "Handle" + getEventTypeName(eventType)
		if method, exists := handlerType.MethodByName(methodName); exists {
			r.handlers[reflect.TypeOf(eventType)] = method
		} else {
			panic(fmt.Sprintf("Missing handler method: %s for event type: %T", methodName, eventType))
		}
	}
}

func (r *EventRegistry) Handle(handler interface{}, msg tea.Msg, stateManager *services.StateManager) (tea.Model, tea.Cmd) {
	method, exists := r.handlers[reflect.TypeOf(msg)]
	if !exists {
		panic(fmt.Sprintf("No handler registered for event type: %T", msg))
	}

	args := []reflect.Value{
		reflect.ValueOf(handler),
		reflect.ValueOf(msg),
		reflect.ValueOf(stateManager),
	}

	results := method.Func.Call(args)

	var model tea.Model
	var cmd tea.Cmd

	if !results[0].IsNil() {
		model = results[0].Interface().(tea.Model)
	}

	if !results[1].IsNil() {
		cmd = results[1].Interface().(tea.Cmd)
	}

	return model, cmd
}

func getEventTypeName(event tea.Msg) string {
	eventType := reflect.TypeOf(event)
	return eventType.Name()
}
