//go:build darwin

package macos

import (
	"container/ring"
	"sync"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// EventBridge multicasts chat events to multiple subscribers
// without modifying the existing event flow to the terminal UI.
type EventBridge struct {
	subscribers []*subscriber
	subMutex    sync.RWMutex
	eventBuffer *ring.Ring
	bufferSize  int
}

type subscriber struct {
	ch     chan domain.ChatEvent
	closed bool
	mu     sync.Mutex
}

// NewEventBridge creates a new event bridge with a circular buffer
func NewEventBridge() *EventBridge {
	bufferSize := 50
	return &EventBridge{
		subscribers: make([]*subscriber, 0),
		eventBuffer: ring.New(bufferSize),
		bufferSize:  bufferSize,
	}
}

// Publish broadcasts an event to all subscribers
// Non-blocking: if a subscriber's channel is full, the event is dropped for that subscriber
func (eb *EventBridge) Publish(event domain.ChatEvent) {
	eb.subMutex.Lock()
	eb.eventBuffer.Value = event
	eb.eventBuffer = eb.eventBuffer.Next()
	subscribers := make([]*subscriber, len(eb.subscribers))
	copy(subscribers, eb.subscribers)
	eb.subMutex.Unlock()

	for _, sub := range subscribers {
		sub.mu.Lock()
		if !sub.closed {
			select {
			case sub.ch <- event:
			default:
			}
		}
		sub.mu.Unlock()
	}
}

// Subscribe creates a new event channel and returns it
// The subscriber will receive all future events published to the bridge
func (eb *EventBridge) Subscribe() chan domain.ChatEvent {
	ch := make(chan domain.ChatEvent, 100)
	sub := &subscriber{
		ch:     ch,
		closed: false,
	}

	eb.subMutex.Lock()
	defer eb.subMutex.Unlock()

	eb.subscribers = append(eb.subscribers, sub)

	eb.eventBuffer.Do(func(val any) {
		if val != nil {
			event, ok := val.(domain.ChatEvent)
			if ok {
				select {
				case ch <- event:
				default:
				}
			}
		}
	})

	return ch
}

// Unsubscribe removes a subscriber and closes its channel
func (eb *EventBridge) Unsubscribe(ch chan domain.ChatEvent) {
	eb.subMutex.Lock()
	defer eb.subMutex.Unlock()

	for i, sub := range eb.subscribers {
		if sub.ch == ch {
			sub.mu.Lock()
			if !sub.closed {
				close(sub.ch)
				sub.closed = true
			}
			sub.mu.Unlock()

			eb.subscribers = append(eb.subscribers[:i], eb.subscribers[i+1:]...)
			break
		}
	}
}

// Tap intercepts an event stream and multicasts it to all subscribers
// Returns a new channel that mirrors the input channel for the terminal UI
func (eb *EventBridge) Tap(input <-chan domain.ChatEvent) <-chan domain.ChatEvent {
	output := make(chan domain.ChatEvent, 100)

	go func() {
		defer close(output)
		for event := range input {
			output <- event
			eb.Publish(event)
		}
	}()

	return output
}

// Close closes all subscriber channels and clears the subscribers list
func (eb *EventBridge) Close() {
	eb.subMutex.Lock()
	defer eb.subMutex.Unlock()

	for _, sub := range eb.subscribers {
		sub.mu.Lock()
		if !sub.closed {
			close(sub.ch)
			sub.closed = true
		}
		sub.mu.Unlock()
	}

	eb.subscribers = nil
}
