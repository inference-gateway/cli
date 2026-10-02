package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

func TestToolsYAMLLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), ToolsFileName)
	if err := SaveTools(path, DefaultToolsConfig()); err != nil {
		t.Fatalf("SaveTools: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var keys []string
	for i := 0; i < len(doc.Content[0].Content); i += 2 {
		keys = append(keys, doc.Content[0].Content[i].Value)
	}
	if want := []string{"enabled", "custom_dir", "max_result_bytes", "safety", "tools"}; !slices.Equal(keys, want) {
		t.Fatalf("top-level keys = %v, want %v", keys, want)
	}

	edited := "---\nenabled: false\ntools:\n  enabled: true\n  bash:\n    timeout: 5\nread:\n  enabled: false\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	cfg, err := LoadTools(path)
	if err != nil {
		t.Fatalf("LoadTools: %v", err)
	}
	if cfg.Enabled {
		t.Error("enabled is a top-level key, an enabled inside tools: must not override it")
	}
	if cfg.Bash.Timeout != 5 {
		t.Errorf("tools.bash.timeout = %d, want 5", cfg.Bash.Timeout)
	}
	if !cfg.Bash.Enabled {
		t.Error("a key the file leaves out keeps its default")
	}
	if !cfg.Read.Enabled {
		t.Error("a tool section outside tools: is ignored")
	}
}
