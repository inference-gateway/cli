package components

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestStatusViewTurnDurationSummary(t *testing.T) {
	tests := []struct {
		name       string
		started    time.Time
		paused     time.Time
		event      func() tea.Msg
		wantText   string
		wantHidden bool
	}{
		{
			name:     "completed turn keeps a Done in summary until the next turn",
			started:  time.Now().Add(-2 * time.Second),
			event:    func() tea.Msg { return agentdomain.ChatCompleteEvent{} },
			wantText: "Done in 2s",
		},
		{
			name:     "cancelled turn keeps a Cancelled after summary",
			started:  time.Now().Add(-2 * time.Second),
			event:    func() tea.Msg { return agentdomain.ChatCompleteEvent{Cancelled: true} },
			wantText: "Cancelled after 2s",
		},
		{
			name:     "failed turn shows Failed after with the error",
			started:  time.Now().Add(-2 * time.Second),
			event:    func() tea.Msg { return agentdomain.ChatErrorEvent{Error: errors.New("boom")} },
			wantText: "Failed after 2s: boom",
		},
		{
			name:     "approval pause held at completion stays excluded like the live counter",
			started:  time.Now().Add(-3 * time.Second),
			paused:   time.Now().Add(-time.Second),
			event:    func() tea.Msg { return agentdomain.ChatCompleteEvent{} },
			wantText: "Done in 2s",
		},
		{
			name:       "a completion without a running turn renders nothing",
			event:      func() tea.Msg { return agentdomain.ChatCompleteEvent{} },
			wantHidden: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sv := NewStatusView(createMockStyleProviderForStatus())
			if !tt.started.IsZero() {
				sv.startTime = tt.started
			}
			if !tt.paused.IsZero() {
				sv.pausedAt = tt.paused
			}
			_, _ = sv.Update(tt.event())

			got := plain(sv.Render())
			if tt.wantHidden {
				if got != "" {
					t.Errorf("expected the summary cleared for a turn that never started, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantText) {
				t.Errorf("Render() = %q, want it to contain %q", got, tt.wantText)
			}

			_, _ = sv.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
			if after := plain(sv.Render()); !strings.Contains(after, tt.wantText) {
				t.Errorf("summary should persist across non-turn messages, got %q", after)
			}
		})
	}
}

func TestStatusViewTurnDurationFormatMatchesToolRows(t *testing.T) {
	sv := NewStatusView(createMockStyleProviderForStatus())
	sv.startTime = time.Now().Add(-75 * time.Second)
	_, _ = sv.Update(agentdomain.ChatCompleteEvent{})

	if got := plain(sv.Render()); !strings.Contains(got, "1m15s") {
		t.Errorf("expected the same long-duration format tool rows use, got %q", got)
	}
}
