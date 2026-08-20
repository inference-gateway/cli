package conversation

import (
	"container/ring"
	"sync"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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
	ch     chan agentdomain.ChatEvent
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

// Publish broadcasts an event to every subscriber. Delivery is non-blocking so
// one slow subscriber can never stall the bus for the others.
func (eb *EventBridge) Publish(event agentdomain.ChatEvent) {
	eb.subMutex.Lock()
	eb.eventBuffer.Value = event
	eb.eventBuffer = eb.eventBuffer.Next()
	subscribers := make([]*subscriber, len(eb.subscribers))
	copy(subscribers, eb.subscribers)
	eb.subMutex.Unlock()

	_, droppable := event.(agentdomain.ChatChunkEvent)
	for _, sub := range subscribers {
		sub.enqueue(event, droppable)
	}
}

// enqueue delivers one event without blocking: a full buffer drops a chunk, but
// a control event evicts the oldest buffered event to guarantee room.
func (s *subscriber) enqueue(event agentdomain.ChatEvent, droppable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- event:
		return
	default:
	}
	if droppable {
		return
	}
	select {
	case <-s.ch:
	default:
	}
	s.ch <- event
}

// Subscribe registers a subscriber and replays the recent-event ring buffer so
// backfill-less subscribers catch up on connect.
func (eb *EventBridge) Subscribe() chan agentdomain.ChatEvent { return eb.subscribe(true) }

// SubscribeFuture is Subscribe without the ring-buffer replay, for subscribers
// that backfill history another way (the extension bridge's conversation
// snapshot) where a replay would double-render the last turn.
func (eb *EventBridge) SubscribeFuture() chan agentdomain.ChatEvent { return eb.subscribe(false) }

func (eb *EventBridge) subscribe(replay bool) chan agentdomain.ChatEvent {
	ch := make(chan agentdomain.ChatEvent, 100)
	sub := &subscriber{ch: ch}

	eb.subMutex.Lock()
	defer eb.subMutex.Unlock()

	eb.subscribers = append(eb.subscribers, sub)

	if replay {
		eb.eventBuffer.Do(func(val any) {
			if event, ok := val.(agentdomain.ChatEvent); ok {
				select {
				case ch <- event:
				default:
				}
			}
		})
	}

	return ch
}

// Unsubscribe removes a subscriber and closes its channel
func (eb *EventBridge) Unsubscribe(ch chan agentdomain.ChatEvent) {
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
func (eb *EventBridge) Tap(input <-chan agentdomain.ChatEvent) <-chan agentdomain.ChatEvent {
	output := make(chan agentdomain.ChatEvent, 100)

	go func() {
		defer close(output)
		for event := range input {
			output <- event   // Forward to TUI
			eb.Publish(event) // Multicast to subscribers
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
