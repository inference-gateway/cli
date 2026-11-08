package mcp_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	root "github.com/inference-gateway/cli/cmd/root"
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

// isolate points HOME and the working directory at fresh temp dirs and blanks
// every INFER_* variable, so a run reads neither this machine's config, nor the
// repository's .infer, nor an override from the developer's environment.
func isolate(t *testing.T) string {
	t.Helper()
	for _, env := range os.Environ() {
		if key, _, ok := strings.Cut(env, "="); ok && strings.HasPrefix(key, "INFER_") {
			t.Setenv(key, "")
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	return home
}

// runInfer runs args through a fresh root the way `infer` runs them, so the
// root loads the config into a Viper of its own before the subcommand starts.
func runInfer(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	command := root.NewCommand()
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetArgs(args)
	err := command.ExecuteContext(t.Context())
	return out.String(), err
}

func TestEveryEditKeepsHeaderReferences(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"disable", []string{"mcp", "disable", "hosted"}},
		{"enable", []string{"mcp", "enable", "hosted"}},
		{"update", []string{"mcp", "update", "hosted", "--description", "Hosted docs"}},
		{"add", []string{"mcp", "add", "docs", "https://docs.example.com/mcp"}},
		{"remove", []string{"mcp", "remove", "other"}},
		{"disable-global", []string{"mcp", "disable-global"}},
		{"enable-global", []string{"mcp", "enable-global"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := isolate(t)
			t.Setenv("MCP_CHECK_KEY", "fake-secret-value")
			path := filepath.Join(home, config.ConfigDirName, config.MCPFileName)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, []byte(seededMCP), 0o600); err != nil {
				t.Fatalf("seed: %v", err)
			}

			out, err := runInfer(t, tt.args...)
			if err != nil {
				t.Fatalf("infer %s: %v\n%s", strings.Join(tt.args, " "), err, out)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if !strings.Contains(string(data), "${MCP_CHECK_KEY}") || strings.Contains(string(data), "fake-secret-value") {
				t.Fatalf("mcp.yaml after %s = %q, want the reference and never its value", tt.name, data)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Fatalf("mcp.yaml mode after %s = %o, want 0600 kept", tt.name, got)
			}
		})
	}
}
