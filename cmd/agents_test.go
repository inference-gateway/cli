package cmd

import (
	"testing"
)

func TestRequiresModel(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		expected bool
	}{
		{"mock-agent does not require a model", "mock-agent", false},
		{"browser-agent requires a model", "browser-agent", true},
		{"google-calendar-agent requires a model", "google-calendar-agent", true},
		{"documentation-agent requires a model", "documentation-agent", true},
		{"n8n-agent requires a model", "n8n-agent", true},
		{"unknown agent requires a model", "unknown-agent", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requiresModel(tt.agent)
			if got != tt.expected {
				t.Errorf("requiresModel(%q) = %v, want %v", tt.agent, got, tt.expected)
			}
		})
	}
}
