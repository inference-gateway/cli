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
