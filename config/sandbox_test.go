package config

import (
	"os"
	"path/filepath"
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
