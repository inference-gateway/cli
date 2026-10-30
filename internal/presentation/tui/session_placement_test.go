package tui

import (
	"strings"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestSessionPlacementSaysWhereTheSessionRuns(t *testing.T) {
	tests := []struct {
		name      string
		placement SessionPlacement
		where     string
		remote    bool
	}{
		{name: "in process", placement: SessionPlacement{}},
		{name: "on the local daemon", placement: SessionPlacement{Daemon: true, Local: "laptop"}, where: "laptop"},
		{name: "on the local daemon by name", placement: SessionPlacement{Daemon: true, Local: "laptop", Environment: "laptop"}, where: "laptop"},
		{name: "on another environment", placement: SessionPlacement{Daemon: true, Local: "laptop", Environment: "k8s-dev"}, where: "k8s-dev", remote: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.placement.Where(); got != tt.where {
				t.Errorf("Where = %q, want %q", got, tt.where)
			}
			if got := tt.placement.Remote(); got != tt.remote {
				t.Errorf("Remote = %v, want %v", got, tt.remote)
			}
		})
	}
}

func TestSessionPlacementUnavailable(t *testing.T) {
	inProcess := SessionPlacement{}
	local := SessionPlacement{Daemon: true, Local: "laptop"}
	remote := SessionPlacement{Daemon: true, Local: "laptop", Environment: "k8s-dev"}
	features := []Feature{FeatureLocalFiles, FeatureCompaction, FeatureHistoryRewind, FeatureBackgroundShell, FeatureTasks}

	for _, feature := range features {
		if reason := inProcess.Unavailable(feature); reason != "" {
			t.Errorf("feature %d is unavailable in process: %q", feature, reason)
		}
		if reason := remote.Unavailable(feature); reason == "" {
			t.Errorf("feature %d is available on a remote session", feature)
		}
	}
	if reason := local.Unavailable(FeatureLocalFiles); reason != "" {
		t.Errorf("this machine's files are unavailable on its own daemon: %q", reason)
	}
	if reason := remote.Unavailable(FeatureLocalFiles); !strings.Contains(reason, "k8s-dev") {
		t.Errorf("reason = %q, want it to name where the session runs", reason)
	}
	for _, feature := range features[1:] {
		if local.Unavailable(feature) == "" {
			t.Errorf("feature %d acts on the TUI's own conversation yet is available in a daemon chat", feature)
		}
	}
}

func TestATurnThatStreamsNothingStillCompletes(t *testing.T) {
	s := NewApplicationState()
	s.StartChatSession("req-1", "m", make(chan agentdomain.ChatEvent))

	if err := s.UpdateChatStatus(ChatStatusCompleted); err != nil {
		t.Fatalf("a turn that streamed nothing could not complete: %v", err)
	}
}
