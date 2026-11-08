package utils_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configutils "github.com/inference-gateway/cli/config/utils"
)

type yamlFileFixture struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

func defaultsForFixture() *yamlFileFixture {
	return &yamlFileFixture{Name: "default", Value: "x"}
}

func TestLoadYAML_MissingFileReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.yaml")

	cfg, err := configutils.LoadYAML(path, "fixture", defaultsForFixture)
	if err != nil {
		t.Fatalf("LoadYAML() error = %v", err)
	}
	if cfg.Name != "default" || cfg.Value != "x" {
		t.Errorf("expected defaults, got %+v", cfg)
	}
}

func TestLoadYAML_ParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.yaml")
	if err := os.WriteFile(path, []byte("name: hello\nvalue: world\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := configutils.LoadYAML(path, "fixture", defaultsForFixture)
	if err != nil {
		t.Fatalf("LoadYAML() error = %v", err)
	}
	if cfg.Name != "hello" || cfg.Value != "world" {
		t.Errorf("got %+v", cfg)
	}
}

func TestLoadYAML_ExpandsEnv(t *testing.T) {
	t.Setenv("YAMLFILE_TEST_VAR", "expanded")
	dir := t.TempDir()
	path := filepath.Join(dir, "f.yaml")
	if err := os.WriteFile(path, []byte("name: ${YAMLFILE_TEST_VAR}\nvalue: v\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := configutils.LoadYAML(path, "fixture", defaultsForFixture)
	if err != nil {
		t.Fatalf("LoadYAML() error = %v", err)
	}
	if cfg.Name != "expanded" {
		t.Errorf("expected env-expanded name, got %q", cfg.Name)
	}
}

func TestLoadYAML_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: ["), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := configutils.LoadYAML(path, "fixture", defaultsForFixture)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "fixture") {
		t.Errorf("error should be label-scoped, got %v", err)
	}
}

func TestSaveYAML_WritesDocMarkerAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "f.yaml")
	original := &yamlFileFixture{Name: "n", Value: "v"}

	if err := configutils.SaveYAML(path, "fixture", original); err != nil {
		t.Fatalf("SaveYAML() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after save: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("---\n")) {
		t.Errorf("expected `---\\n` doc marker prefix, got %q", string(data[:min(8, len(data))]))
	}

	loaded, err := configutils.LoadYAML(path, "fixture", defaultsForFixture)
	if err != nil {
		t.Fatalf("load after save: %v", err)
	}
	if *loaded != *original {
		t.Errorf("round-trip mismatch: got %+v want %+v", loaded, original)
	}
}

func TestSaveYAML_CreatesParentDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "f.yaml")

	if err := configutils.SaveYAML(path, "fixture", &yamlFileFixture{Name: "n"}); err != nil {
		t.Fatalf("SaveYAML() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestLoadYAMLAsWritten_KeepsReferencesThroughASave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.yaml")
	if err := os.WriteFile(path, []byte("name: ${YAMLFILE_TEST_VAR}\nvalue: v\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Setenv("YAMLFILE_TEST_VAR", "resolved-secret")

	cfg, err := configutils.LoadYAMLAsWritten(path, "fixture", defaultsForFixture)
	if err != nil {
		t.Fatalf("LoadYAMLAsWritten() error = %v", err)
	}
	if cfg.Name != "${YAMLFILE_TEST_VAR}" {
		t.Fatalf("Name = %q, want the reference as written", cfg.Name)
	}
	cfg.Value = "edited"
	if err := configutils.SaveYAML(path, "fixture", cfg); err != nil {
		t.Fatalf("SaveYAML() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(data), "${YAMLFILE_TEST_VAR}") || strings.Contains(string(data), "resolved-secret") {
		t.Fatalf("saved file = %q, want the reference and never its value", data)
	}
}

func TestSaveYAML_KeepsTheFileMode(t *testing.T) {
	dir := t.TempDir()
	tightened := filepath.Join(dir, "tightened.yaml")
	if err := os.WriteFile(tightened, []byte("name: a\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tests := []struct {
		name string
		path string
		want os.FileMode
	}{
		{"a file its owner tightened stays tight", tightened, 0o600},
		{"a new file is readable by all", filepath.Join(dir, "new.yaml"), 0o644},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := configutils.SaveYAML(tt.path, "fixture", defaultsForFixture()); err != nil {
				t.Fatalf("SaveYAML() error = %v", err)
			}
			info, err := os.Stat(tt.path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if got := info.Mode().Perm(); got != tt.want {
				t.Errorf("mode = %o, want %o", got, tt.want)
			}
		})
	}
}
