package config

import "testing"

func TestResolveOCITag(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		tag      string
		expected string
		wantErr  bool
	}{
		{"browser engine variant", "browser-agent", "lightpanda", "ghcr.io/inference-gateway/browser-agent:lightpanda", false},
		{"pinned version", "browser-agent", "chromium-0.8.0", "ghcr.io/inference-gateway/browser-agent:chromium-0.8.0", false},
		{"another known agent", "mock-agent", "0.4.0", "ghcr.io/inference-gateway/mock-agent:0.4.0", false},
		{"unknown agent", "code-reviewer", "latest", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveOCITag(tt.agent, tt.tag)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveOCITag(%q, %q) error = %v, wantErr %v", tt.agent, tt.tag, err, tt.wantErr)
			}
			if got != tt.expected {
				t.Errorf("ResolveOCITag(%q, %q) = %q, want %q", tt.agent, tt.tag, got, tt.expected)
			}
		})
	}
}

func TestAgentRequiresModel(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		run      bool
		expected bool
	}{
		{"mock-agent does not require a model even when run locally", "mock-agent", true, false},
		{"browser-agent requires a model when run locally", "browser-agent", true, true},
		{"google-calendar-agent requires a model when run locally", "google-calendar-agent", true, true},
		{"documentation-agent requires a model when run locally", "documentation-agent", true, true},
		{"n8n-agent requires a model when run locally", "n8n-agent", true, true},
		{"unknown agent requires a model when run locally", "unknown-agent", true, true},
		{"mock-agent does not require a model when not run locally", "mock-agent", false, false},
		{"browser-agent does not require a model when not run locally", "browser-agent", false, false},
		{"unknown agent does not require a model when not run locally", "unknown-agent", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AgentRequiresModel(tt.agent, tt.run)
			if got != tt.expected {
				t.Errorf("AgentRequiresModel(%q, %v) = %v, want %v", tt.agent, tt.run, got, tt.expected)
			}
		})
	}
}
