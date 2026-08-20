package app

import (
	"testing"

	spinner "charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	ui "github.com/inference-gateway/cli/internal/ui"
)

// TestShouldRouteToUIComponents guards the event-routing predicate against
// package moves: when UI events lived in internal/domain, a stringly-typed
// "domain." prefix check silently dropped every event after the move to
// internal/ui, and no reply ever rendered in the TUI.
func TestShouldRouteToUIComponents(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.Msg
		want bool
	}{
		{"ui update history", ui.UpdateHistoryEvent{}, true},
		{"ui streaming content", ui.StreamingContentEvent{}, true},
		{"ui set status", ui.SetStatusEvent{}, true},
		{"ui show error", ui.ShowErrorEvent{}, true},
		{"ui clear input", ui.ClearInputEvent{}, true},
		{"ui agent status update", ui.AgentStatusUpdateEvent{}, true},
		{"ui git pr resolved", ui.GitPRResolvedEvent{}, true},
		{"ui todo update", ui.TodoUpdateEvent{}, true},
		{"agent chat chunk", agentdomain.ChatChunkEvent{}, true},
		{"agent chat complete", agentdomain.ChatCompleteEvent{}, true},
		{"agent tool call update", agentdomain.ToolCallUpdateEvent{}, true},
		{"spinner tick", spinner.TickMsg{}, true},
		{"nil message", nil, false},
		{"foreign message", struct{ X int }{}, false},
		{"plain string", "not an event", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRouteToUIComponents(tt.msg); got != tt.want {
				t.Fatalf("shouldRouteToUIComponents(%T) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}
