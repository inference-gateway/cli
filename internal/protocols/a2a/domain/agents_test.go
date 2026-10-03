package domain

import "testing"

func TestAgentStateNames(t *testing.T) {
	tests := []struct {
		s           AgentState
		want        string
		wantDisplay string
	}{
		{AgentStateUnknown, "Unknown", "unknown"},
		{AgentStatePullingImage, "PullingImage", "pulling image"},
		{AgentStateStarting, "Starting", "starting"},
		{AgentStateWaitingReady, "WaitingReady", "waiting"},
		{AgentStateReady, "Ready", "ready"},
		{AgentStateFailed, "Failed", "failed"},
		{AgentStateRemoved, "Removed", "removed"},
		{AgentState(99), "Unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Errorf("AgentState(%d).String() = %q, want %q", tt.s, got, tt.want)
			}
			if got := tt.s.DisplayName(); got != tt.wantDisplay {
				t.Errorf("AgentState(%d).DisplayName() = %q, want %q", tt.s, got, tt.wantDisplay)
			}
		})
	}
}

func TestAgentChangesString(t *testing.T) {
	changes := AgentChanges{Added: []string{"mock-agent"}, Removed: []string{"old"}, Restarted: []string{"browser-agent"}}
	if got, want := changes.String(), "agents +mock-agent -old ~browser-agent"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if changes.IsEmpty() || !(AgentChanges{}).IsEmpty() {
		t.Error("IsEmpty must be true only without changes")
	}
}
