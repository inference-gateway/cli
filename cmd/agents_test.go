package cmd

import (
	"testing"

	cobra "github.com/spf13/cobra"
)

func TestResolveTagFlag(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		args     []string
		expected string
		wantErr  bool
	}{
		{"no tag flag is a no-op", "browser-agent", nil, "", false},
		{"tag resolves against the default image", "browser-agent", []string{"--tag", "lightpanda"}, "ghcr.io/inference-gateway/browser-agent:lightpanda", false},
		{"tag and oci are mutually exclusive", "browser-agent", []string{"--tag", "lightpanda", "--oci", "ghcr.io/org/other:v1"}, "", true},
		{"tag on an unknown agent", "code-reviewer", []string{"--tag", "latest"}, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.Flags().String("tag", "", "")
			cmd.Flags().String("oci", "", "")
			if err := cmd.ParseFlags(tt.args); err != nil {
				t.Fatalf("ParseFlags(%v) failed: %v", tt.args, err)
			}

			got, err := resolveTagFlag(cmd, tt.agent)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveTagFlag(%q) error = %v, wantErr %v", tt.agent, err, tt.wantErr)
			}
			if got != tt.expected {
				t.Errorf("resolveTagFlag(%q) = %q, want %q", tt.agent, got, tt.expected)
			}
		})
	}
}

func TestRequiresModel(t *testing.T) {
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
			got := requiresModel(tt.agent, tt.run)
			if got != tt.expected {
				t.Errorf("requiresModel(%q, %v) = %v, want %v", tt.agent, tt.run, got, tt.expected)
			}
		})
	}
}
