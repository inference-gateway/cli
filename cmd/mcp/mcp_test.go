package mcp

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
)

const seededMCP = `enabled: true
servers:
  - name: hosted
    enabled: true
    url: https://hosted.example.com/mcp
    headers:
      x-api-key: ${MCP_CHECK_KEY}
  - name: other
    enabled: true
    url: https://other.example.com/mcp
`

func TestEveryEditKeepsHeaderReferences(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"disable", []string{"disable", "hosted"}},
		{"enable", []string{"enable", "hosted"}},
		{"update", []string{"update", "hosted", "--description", "Hosted docs"}},
		{"add", []string{"add", "docs", "https://docs.example.com/mcp"}},
		{"remove", []string{"remove", "other"}},
		{"disable-global", []string{"disable-global"}},
		{"enable-global", []string{"enable-global"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("MCP_CHECK_KEY", "fake-secret-value")
			path := filepath.Join(home, config.ConfigDirName, config.MCPFileName)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, []byte(seededMCP), 0o600); err != nil {
				t.Fatalf("seed: %v", err)
			}

			command := NewCommand(runtime.NewState(), output.NewRenderer())
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			command.SetArgs(tt.args)
			if err := command.ExecuteContext(t.Context()); err != nil {
				t.Fatalf("infer mcp %s: %v", strings.Join(tt.args, " "), err)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if !strings.Contains(string(data), "${MCP_CHECK_KEY}") || strings.Contains(string(data), "fake-secret-value") {
				t.Fatalf("mcp.yaml after %s = %q, want the reference and never its value", tt.name, data)
			}
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("mcp.yaml mode after %s = %v (%v), want 0600 kept", tt.name, info.Mode().Perm(), err)
			}
		})
	}
}
