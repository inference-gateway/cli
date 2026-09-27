package tools

import (
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestMarkdownAgentSubagentInfo(t *testing.T) {
	tests := []struct {
		name     string
		agent    markdownAgent
		expected agentdomain.SubagentInfo
	}{
		{
			name: "read-only allowlist",
			agent: markdownAgent{
				name:        "explorer",
				description: "Read-only explorer",
				tools:       []string{"Grep", "Read"},
				source:      "project",
			},
			expected: agentdomain.SubagentInfo{
				Name:        "explorer",
				Description: "Read-only explorer",
				Tools:       []string{"Grep", "Read"},
				ReadOnly:    true,
				Source:      "project",
			},
		},
		{
			name: "mutating allowlist is read-write",
			agent: markdownAgent{
				name:        "runner",
				description: "Runs tests",
				tools:       []string{"Bash", "Read"},
				source:      "user",
			},
			expected: agentdomain.SubagentInfo{
				Name:        "runner",
				Description: "Runs tests",
				Tools:       []string{"Bash", "Read"},
				ReadOnly:    false,
				Source:      "user",
			},
		},
		{
			name: "no allowlist inherits the parent and runs read-write",
			agent: markdownAgent{
				name:        "writer",
				description: "Free-form helper",
				model:       "anthropic/claude-4-5-sonnet",
				source:      "user",
			},
			expected: agentdomain.SubagentInfo{
				Name:        "writer",
				Description: "Free-form helper",
				Model:       "anthropic/claude-4-5-sonnet",
				ReadOnly:    false,
				Source:      "user",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.agent.subagentInfo()
			if got.Name != tt.expected.Name || got.Description != tt.expected.Description ||
				got.Model != tt.expected.Model || got.ReadOnly != tt.expected.ReadOnly ||
				got.Source != tt.expected.Source {
				t.Errorf("subagentInfo() = %+v, want %+v", got, tt.expected)
			}
		})
	}
}

func TestSubagentSourceLabel(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}

	if got := subagentSourceLabel(filepath.Join(home, config.ConfigDirName, "agents")); got != "user" {
		t.Errorf("home agents dir classified as %q, want user", got)
	}
	if got := subagentSourceLabel(t.TempDir()); got != "project" {
		t.Errorf("temp dir classified as %q, want project", got)
	}
}
