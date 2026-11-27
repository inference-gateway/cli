package teleport

import (
	"errors"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestPlacementReportsTheTeleportsFirstOutcome(t *testing.T) {
	tests := []struct {
		name    string
		frame   any
		want    error
		reports bool
	}{
		{"the session landed", sessionTeleportedFrame{Type: frameSessionTeleported}, nil, true},
		{"a stage failed", progressFrame{Type: frameTeleportProgress, TeleportProgress: agentdomain.TeleportProgress{Stage: agentdomain.TeleportFailed, Message: "no room"}}, errors.New("no room"), true},
		{"the request was refused", map[string]string{"type": frameRunError, "message": "bad frame"}, errors.New("bad frame"), true},
		{"progress goes on", progressFrame{Type: frameTeleportProgress, TeleportProgress: agentdomain.TeleportProgress{Stage: agentdomain.TeleportReady}}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			landed := make(chan error, 1)
			placement(landed).Deliver(encode(tt.frame))
			select {
			case err := <-landed:
				if !tt.reports || (err == nil) != (tt.want == nil) || (err != nil && err.Error() != tt.want.Error()) {
					t.Fatalf("reported %v, want %v (reports %v)", err, tt.want, tt.reports)
				}
			default:
				if tt.reports {
					t.Fatalf("reported nothing, want %v", tt.want)
				}
			}
		})
	}
}

func TestPlaceReturnsWhyTheSessionDidNotMove(t *testing.T) {
	s := New(Config{Identity: "laptop", ProjectsRoot: t.TempDir()}, Deps{})

	err := s.Place(t.Context(), t.TempDir(), "room-public-editor", "nowhere")

	if err == nil {
		t.Fatal("placing on an unknown workspace succeeded")
	}
}
