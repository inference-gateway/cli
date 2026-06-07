package states

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestCompletingState_IgnoresNonCompletionEvents verifies that the Completing
// state finalizes only on CompletionRequestedEvent. A stray event (e.g. a
// MessageReceivedEvent) must be a no-op so it cannot trigger completion or
// enqueue any follow-up event.
func TestCompletingState_IgnoresNonCompletionEvents(t *testing.T) {
	events := make(chan domain.AgentEvent, 1)
	s := NewCompletingState(&domain.StateContext{Events: events})

	err := s.Handle(domain.MessageReceivedEvent{})

	assert.NoError(t, err)
	assert.Empty(t, events, "non-completion event must not enqueue any follow-up event")
}
