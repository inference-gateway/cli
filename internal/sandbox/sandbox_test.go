package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

func TestSandboxDeniedPath(t *testing.T) {
	tests := []struct {
		name     string
		msg      string
		wantPath string
		wantOK   bool
	}{
		{"denial message", (&sandboxdomain.DeniedError{Path: "/tmp/x"}).Error(), "/tmp/x", true},
		{"wrapped in tool failure", "Tool execution failed: config.Read - path '/etc/hosts' is outside configured sandbox directories", "/etc/hosts", true},
		{"unrelated error", "file not found", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := sandboxdomain.DeniedPath(tt.msg)
			if ok != tt.wantOK || path != tt.wantPath {
				t.Fatalf("sandboxdomain.DeniedPath(%q) = (%q, %v), want (%q, %v)", tt.msg, path, ok, tt.wantPath, tt.wantOK)
			}
		})
	}
}

func TestAddSandboxDirectoryGrantsAccessWithoutChangingPromptList(t *testing.T) {
	t.Cleanup(func() {
		sandboxdomain.Granted = sandboxdomain.Grants{}
	})

	sandbox := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "file.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Directories = []string{sandbox}
	cfg.Tools.Sandbox.ProtectedPaths = nil

	if err := ValidateRead(cfg, target); err == nil {
		t.Fatal("expected denial before grant")
	}

	sandboxdomain.Granted.Add(outside)

	if err := ValidateRead(cfg, target); err != nil {
		t.Fatalf("expected access after grant, got %v", err)
	}
	if got := cfg.GetSandboxDirectories(); len(got) != 1 || got[0] != sandbox {
		t.Fatalf("GetSandboxDirectories must stay prompt-stable, got %v", got)
	}
}

func TestValidatePathInSandbox_Symlinks(t *testing.T) {
	t.Cleanup(func() {
		sandboxdomain.Granted = sandboxdomain.Grants{}
	})

	sandbox := t.TempDir()
	outside := t.TempDir()
	outsideDir := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	mustWrite(t, secret)
	mustWrite(t, filepath.Join(sandbox, "inside.txt"))
	mustWrite(t, filepath.Join(sandbox, "id_ed25519"))
	mustSymlink(t, filepath.Join(sandbox, "inside.txt"), filepath.Join(sandbox, "file-in"))
	mustSymlink(t, secret, filepath.Join(sandbox, "file-out"))
	mustSymlink(t, outsideDir, filepath.Join(sandbox, "dir-out"))
	mustSymlink(t, filepath.Join(sandbox, "id_ed25519"), filepath.Join(sandbox, "notes.txt"))
	sandboxLink := filepath.Join(t.TempDir(), "sandbox-link")
	mustSymlink(t, sandbox, sandboxLink)

	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Directories = []string{sandbox}

	tests := []struct {
		name       string
		dirs       []string
		path       string
		wantDenied string
		protected  bool
	}{
		{name: "a regular file", path: filepath.Join(sandbox, "inside.txt")},
		{name: "a link to a file inside", path: filepath.Join(sandbox, "file-in")},
		{name: "a link to a file outside", path: filepath.Join(sandbox, "file-out"), wantDenied: config.RealPath(secret)},
		{name: "a new file through a linked dir", path: filepath.Join(sandbox, "dir-out", "new.txt"), wantDenied: filepath.Join(config.RealPath(outsideDir), "new.txt")},
		{name: "a link to a protected file", path: filepath.Join(sandbox, "notes.txt"), protected: true},
		{name: "a file through a symlinked sandbox dir", dirs: []string{sandboxLink}, path: filepath.Join(sandboxLink, "inside.txt")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.dirs != nil {
				cfg := *cfg
				cfg.Tools.Sandbox.Directories = tt.dirs
				if err := ValidateRead(&cfg, tt.path); err != nil {
					t.Fatalf("expected %s allowed, got %v", tt.path, err)
				}
				return
			}
			err := ValidateWrite(cfg, tt.path)
			var denied *sandboxdomain.DeniedError
			switch {
			case tt.protected:
				if err == nil || !strings.Contains(err.Error(), "excluded for security") {
					t.Fatalf("expected %s protected, got %v", tt.path, err)
				}
			case tt.wantDenied != "":
				if !errors.As(err, &denied) || denied.Path != tt.wantDenied {
					t.Fatalf("expected denial of %s, got %v", tt.wantDenied, err)
				}
				sandboxdomain.Granted.Add(filepath.Dir(denied.Path))
				if err := ValidateWrite(cfg, tt.path); err != nil {
					t.Fatalf("expected %s allowed once its target dir is granted, got %v", tt.path, err)
				}
			case err != nil:
				t.Fatalf("expected %s allowed, got %v", tt.path, err)
			}
		})
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
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

	cfg := config.DefaultConfig()
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
			err := ValidateWrite(cfg, tt.path)
			if blocked := err != nil && strings.Contains(err.Error(), "custom tools directory"); blocked != tt.blocked {
				t.Errorf("ValidateWrite(%s) = %v, want blocked %v", tt.path, err, tt.blocked)
			}
		})
	}
}
