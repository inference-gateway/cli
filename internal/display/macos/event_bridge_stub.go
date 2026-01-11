//go:build !darwin

package macos

import (
	domain "github.com/inference-gateway/cli/internal/domain"
)

// EventBridge stub for non-darwin platforms
type EventBridge struct{}

// NewEventBridge creates a stub event bridge
func NewEventBridge() *EventBridge {
	return &EventBridge{}
}

// Tap returns the input channel unchanged on non-darwin platforms
func (eb *EventBridge) Tap(input <-chan domain.ChatEvent) <-chan domain.ChatEvent {
	return input
}

// Publish is a no-op on non-darwin platforms
func (eb *EventBridge) Publish(event domain.ChatEvent) {}

// Subscribe returns a dummy channel that never receives events
func (eb *EventBridge) Subscribe() chan domain.ChatEvent {
	return make(chan domain.ChatEvent)
}

// Unsubscribe is a no-op on non-darwin platforms
func (eb *EventBridge) Unsubscribe(ch chan domain.ChatEvent) {}

// Close is a no-op on non-darwin platforms
func (eb *EventBridge) Close() {}
