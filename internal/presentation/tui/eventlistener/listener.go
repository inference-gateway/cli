package eventlistener

import (
	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

// Service is the shared implementation of ui.ChatEventListener used by
// every handler/service that needs to keep pumping a Bubble Tea channel.
type Service struct{}

// NewService returns a new event listener service.
func NewService() *Service {
	return &Service{}
}

// ListenForChatEvents returns a tea.Cmd that reads one event off the chat
// event channel and surfaces it as a tui.ChatChannelEvent, which the chat
// handler unwraps and re-arms. A closed channel terminates the listener
// (returns nil).
func (s *Service) ListenForChatEvents(eventChan <-chan agentdomain.ChatEvent) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-eventChan; ok {
			return tui.ChatChannelEvent{Event: event, Source: eventChan}
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
