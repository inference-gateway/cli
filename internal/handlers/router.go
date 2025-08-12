package handlers

import (
	"sort"

	"github.com/charmbracelet/bubbletea"
)

// MessageRouter routes messages to appropriate handlers
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

	// Sort handlers by priority (higher priority first)
	sort.Slice(r.handlers, func(i, j int) bool {
		return r.handlers[i].GetPriority() > r.handlers[j].GetPriority()
	})
}

// RemoveHandler removes a message handler from the router
func (r *MessageRouter) RemoveHandler(handler MessageHandler) {
	for i, h := range r.handlers {
		if h == handler {
			r.handlers = append(r.handlers[:i], r.handlers[i+1:]...)
			break
		}
	}
}

// Route routes a message to the appropriate handler
func (r *MessageRouter) Route(msg tea.Msg, state *AppState) (tea.Model, tea.Cmd) {
	for _, handler := range r.handlers {
		if handler.CanHandle(msg) {
			return handler.Handle(msg, state)
		}
	}

	// No handler found - return nil to indicate no action
	return nil, nil
}

// GetHandlers returns all registered handlers
func (r *MessageRouter) GetHandlers() []MessageHandler {
	result := make([]MessageHandler, len(r.handlers))
	copy(result, r.handlers)
	return result
}
