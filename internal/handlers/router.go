package handlers

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/internal/services"
)

// MessageRouter routes messages to handlers using the new state management system
type MessageRouter struct {
	handlers []MessageHandler
}

// NewMessageRouter creates a new message router
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{
		handlers: make([]MessageHandler, 0),
	}
}

// AddHandler adds a message handler to the router
func (r *MessageRouter) AddHandler(handler MessageHandler) {
	r.handlers = append(r.handlers, handler)
	sort.Slice(r.handlers, func(i, j int) bool {
		return r.handlers[i].GetPriority() > r.handlers[j].GetPriority()
	})
}

// RemoveHandler removes a message handler from the router
func (r *MessageRouter) RemoveHandler(handlerName string) {
	for i, handler := range r.handlers {
		if handler.GetName() == handlerName {
			r.handlers = append(r.handlers[:i], r.handlers[i+1:]...)
			break
		}
	}
}

// Route routes a message to the appropriate handler
func (r *MessageRouter) Route(
	msg tea.Msg,
	stateManager *services.StateManager,
) (tea.Model, tea.Cmd) {
	for _, handler := range r.handlers {
		if handler.CanHandle(msg) {
			model, cmd := handler.Handle(msg, stateManager)
			return model, cmd
		}
	}

	return nil, nil
}

// GetHandlers returns all registered handlers
func (r *MessageRouter) GetHandlers() []MessageHandler {
	handlers := make([]MessageHandler, len(r.handlers))
	copy(handlers, r.handlers)
	return handlers
}

// GetHandlerByName returns a handler by name
func (r *MessageRouter) GetHandlerByName(name string) MessageHandler {
	for _, handler := range r.handlers {
		if handler.GetName() == name {
			return handler
		}
	}
	return nil
}

// GetHandlerCount returns the number of registered handlers
func (r *MessageRouter) GetHandlerCount() int {
	return len(r.handlers)
}
