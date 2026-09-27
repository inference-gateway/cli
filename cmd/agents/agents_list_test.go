package agents

import (
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestMarkdownAgentTools(t *testing.T) {
	tests := []struct {
		name string
		info agentdomain.SubagentInfo
		want string
	}{
		{
			name: "no allowlist inherits the parent tools",
			info: agentdomain.SubagentInfo{Name: "runner"},
			want: "all tools (inherited)",
		},
		{
			name: "allowlist is joined",
			info: agentdomain.SubagentInfo{Name: "explorer", Tools: []string{"Grep", "Read"}},
			want: "Grep, Read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := markdownAgentTools(tt.info); got != tt.want {
				t.Errorf("markdownAgentTools() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMarkdownAgentMode(t *testing.T) {
	tests := []struct {
		name string
		info agentdomain.SubagentInfo
		want string
	}{
		{
			name: "default is read-write",
			info: agentdomain.SubagentInfo{Name: "runner"},
			want: "read-write",
		},
		{
			name: "readonly preset is read-only",
			info: agentdomain.SubagentInfo{Name: "explorer", ReadOnly: true},
			want: "read-only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := markdownAgentMode(tt.info); got != tt.want {
				t.Errorf("markdownAgentMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
