package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxDeniedPath(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		wantPath string
		wantOK   bool
	}{
		{"denial message", (&SandboxPathError{Path: "/tmp/x"}).Error(), "/tmp/x", true},
		{"wrapped in tool failure", "Tool execution failed: Read - path '/etc/hosts' is outside configured sandbox directories", "/etc/hosts", true},
		{"unrelated error", "file not found", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := SandboxDeniedPath(tt.msg)
			if ok != tt.wantOK || path != tt.wantPath {
				t.Fatalf("SandboxDeniedPath(%q) = (%q, %v), want (%q, %v)", tt.msg, path, ok, tt.wantPath, tt.wantOK)
			}
		})
	}
}

func TestAddSandboxDirectoryGrantsAccessWithoutChangingPromptList(t *testing.T) {
	t.Cleanup(func() {
		sandboxGrantsMu.Lock()
		sandboxGrants = nil
		sandboxGrantsMu.Unlock()
	})

	sandbox := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "file.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Tools.Sandbox.Directories = []string{sandbox}
	cfg.Tools.Sandbox.ProtectedPaths = nil

	if err := cfg.ValidatePathInSandbox(target); err == nil {
		t.Fatal("expected denial before grant")
	}

	AddSandboxDirectory(outside)

	if err := cfg.ValidatePathInSandbox(target); err != nil {
		t.Fatalf("expected access after grant, got %v", err)
	}
	if got := cfg.GetSandboxDirectories(); len(got) != 1 || got[0] != sandbox {
		t.Fatalf("GetSandboxDirectories must stay prompt-stable, got %v", got)
	}
}

func TestValidatePathInSandboxWrite_CustomToolsDirs(t *testing.T) {
	project := t.TempDir()
	t.Chdir(project)
	userTools := filepath.Join(t.TempDir(), "my-tools")
	if err := os.MkdirAll(filepath.Join(project, ".agents", "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(project, ".agents", "tools"), filepath.Join(project, "docs")); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.Tools.Sandbox.Directories = []string{project, filepath.Dir(userTools)}
	cfg.Tools.CustomDir = userTools

	tests := []struct {
		name    string
		path    string
		blocked bool
	}{
		{name: "project .infer/tools", path: filepath.Join(".infer", "tools", "Evil.yaml"), blocked: true},
		{name: "project .agents/tools", path: filepath.Join(".agents", "tools", "Evil.yaml"), blocked: true},
		{name: "the tools directory itself", path: filepath.Join(".agents", "tools"), blocked: true},
		{name: "another spelling", path: filepath.Join(".Agents", "Tools", "Evil.yaml"), blocked: true},
		{name: "through a symlink", path: filepath.Join("docs", "Evil.yaml"), blocked: true},
		{name: "tools.custom_dir", path: filepath.Join(userTools, "Evil.yaml"), blocked: true},
		{name: "a sibling of the tools directory", path: filepath.Join(".agents", "tools-notes.md")},
		{name: "a project file", path: "main.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cfg.ValidatePathInSandboxWrite(tt.path)
			if blocked := err != nil && strings.Contains(err.Error(), "custom tools directory"); blocked != tt.blocked {
				t.Errorf("ValidatePathInSandboxWrite(%s) = %v, want blocked %v", tt.path, err, tt.blocked)
			}
		})
	}
}
