package eventlistener

import (
	tea "charm.land/bubbletea/v2"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// Service is the shared implementation of domain.ChatEventListener used by
// every handler/service that needs to keep pumping a Bubble Tea channel.
type Service struct{}

// NewService returns a new event listener service.
func NewService() *Service {
	return &Service{}
}

// ListenForChatEvents returns a tea.Cmd that reads one event off the chat
// event channel and surfaces it as the next tea.Msg. A closed channel
// terminates the listener (returns nil).
func (s *Service) ListenForChatEvents(eventChan <-chan domain.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-eventChan; ok {
			return event
		}
		return nil
	}
}

// ListenForEvents is the tea.Msg variant of ListenForChatEvents, used for
// non-chat channels (bash output, tool progress).
func (s *Service) ListenForEvents(eventChan <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-eventChan
		if !ok {
			return nil
		}
		return msg
	}
}
