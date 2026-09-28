package customtools

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

const echoManifest = `name: Echo
description: Echoes its input.
command: [bin/echo-tool, --verbose]
parameters:
  type: object
  properties:
    text:
      type: string
  required: [text]
`

var builtins = []string{"Read", "WebSearch"}

func loadTools(t *testing.T, files map[string]string) map[string]*Tool {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Tools: config.ToolsConfig{CustomDir: dir}}
	loaded := make(map[string]*Tool)
	for name, tool := range NewTools(cfg, builtins) {
		loaded[name] = tool.(*Tool)
	}
	return loaded
}

func TestNewTools_LoadsValidManifest(t *testing.T) {
	tool, ok := loadTools(t, map[string]string{"Echo.yaml": echoManifest})["Echo"]
	if !ok {
		t.Fatal("Echo was not loaded")
	}
	if tool.timeout != defaultTimeout {
		t.Errorf("timeout = %s, want the default %s", tool.timeout, defaultTimeout)
	}
	manifest := tool.Manifest()
	if manifest.RequireApproval != nil || manifest.Modes != nil {
		t.Errorf("manifest = %+v, want require_approval and modes left to inherit", manifest)
	}
	if !filepath.IsAbs(tool.command[0]) || !strings.HasSuffix(tool.command[0], filepath.Join("bin", "echo-tool")) || tool.command[1] != "--verbose" {
		t.Errorf("command = %v, want the relative program resolved against the manifest's directory", tool.command)
	}
}

func TestNewTools_BareCommandStaysOnPath(t *testing.T) {
	manifest := strings.Replace(echoManifest, "bin/echo-tool", "echo-tool", 1)
	tool := loadTools(t, map[string]string{"Echo.yaml": manifest})["Echo"]
	if tool == nil || tool.command[0] != "echo-tool" {
		t.Fatalf("tool = %+v, want the bare program name left for a PATH lookup", tool)
	}
}

func TestNewTools_SkipsInvalidManifests(t *testing.T) {
	renamed := func(name string) string {
		return strings.Replace(echoManifest, "name: Echo", "name: "+name, 1)
	}
	tests := []struct {
		name string
		file string
		yaml string
	}{
		{name: "unknown field", file: "Echo.yaml", yaml: echoManifest + "read_only: true\n"},
		{name: "invalid yaml", file: "Echo.yaml", yaml: "name: [Echo"},
		{name: "file name mismatch", file: "Other.yaml", yaml: echoManifest},
		{name: "built-in name", file: "Read.yaml", yaml: renamed("Read")},
		{name: "built-in name in another case", file: "read.yaml", yaml: renamed("read")},
		{name: "built-in switched off in config", file: "WebSearch.yaml", yaml: renamed("WebSearch")},
		{name: "MCP prefix", file: "MCP_github_search.yaml", yaml: renamed("MCP_github_search")},
		{name: "missing command", file: "Echo.yaml", yaml: strings.Replace(echoManifest, "command: [bin/echo-tool, --verbose]\n", "", 1)},
		{name: "negative timeout", file: "Echo.yaml", yaml: echoManifest + "timeout: -1\n"},
		{name: "disabled", file: "Echo.yaml", yaml: echoManifest + "enabled: false\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := strings.Replace(echoManifest, "name: Echo", "name: Valid", 1)
			tools := loadTools(t, map[string]string{tt.file: tt.yaml, "Valid.yaml": valid})
			if len(tools) != 1 || tools["Valid"] == nil {
				t.Errorf("loaded %v, want only the valid manifest", slices.Collect(maps.Keys(tools)))
			}
		})
	}
}

func TestNewTools_MissingDirectory(t *testing.T) {
	cfg := &config.Config{Tools: config.ToolsConfig{CustomDir: filepath.Join(t.TempDir(), "absent")}}
	if tools := NewTools(cfg, builtins); len(tools) != 0 {
		t.Errorf("loaded %d tools from a missing directory, want none", len(tools))
	}
}
