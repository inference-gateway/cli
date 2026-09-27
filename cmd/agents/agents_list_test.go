package agents

import (
	"io"
	"os"
	"strings"
	"testing"

	output "github.com/inference-gateway/cli/cmd/output"
	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// captureStdout runs fn with os.Stdout swapped for a pipe and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	fn()
	os.Stdout = old
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured output: %v", err)
	}
	return string(out)
}

func TestPrintA2AAgentsTable(t *testing.T) {
	c := &command{renderer: output.NewRenderer()}

	got := captureStdout(t, func() {
		c.printA2AAgents(2, []config.AgentEntry{
			{
				Name:        "browser-agent",
				URL:         "http://localhost:8080",
				OCI:         "ghcr.io/inference-gateway/browser-agent:0.5.0",
				Run:         true,
				Model:       "openai/gpt-5",
				Environment: map[string]string{"A2A_DEBUG": "true"},
			},
		}, []ExternalAgent{{Name: "remote-agent", URL: "https://agent.example.com"}})
	})

	for _, want := range []string{"A2A Agents (2)", "browser-agent", "yaml", "browser-agent:0.5.0", "remote-agent", "env"} {
		if !strings.Contains(got, want) {
			t.Errorf("A2A table missing %q\noutput:\n%s", want, got)
		}
	}
}

func TestPrintMarkdownAgentsTable(t *testing.T) {
	c := &command{renderer: output.NewRenderer()}

	got := captureStdout(t, func() {
		c.printMarkdownAgents([]agentdomain.SubagentInfo{
			{Name: "runner", Description: "Runs the test suite", Source: "user"},
			{Name: "explorer", Description: "Read-only code explorer", Tools: []string{"Grep", "Read"}, ReadOnly: true, Source: "project"},
		})
	})

	for _, want := range []string{"Markdown Agents (2)", "runner", "all tools (inherited)", "inherit", "explorer", "Grep, Read", "read-only", "project"} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown table missing %q\noutput:\n%s", want, got)
		}
	}
}

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
