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

func TestParseDenial(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want *sandboxdomain.DeniedError
	}{
		{"outside allowed", (&sandboxdomain.DeniedError{Path: "/tmp/x", Access: sandboxdomain.AccessWrite}).Error(), &sandboxdomain.DeniedError{Path: "/tmp/x", Access: sandboxdomain.AccessWrite}},
		{"denied entry", (&sandboxdomain.DeniedError{Path: "deploy/a", Access: sandboxdomain.AccessRead, Rule: "deploy/"}).Error(), &sandboxdomain.DeniedError{Path: "deploy/a", Access: sandboxdomain.AccessRead, Rule: "deploy/"}},
		{"wrapped in tool failure", "Tool execution failed: Read - read to path '/etc/hosts' is denied by the sandbox", &sandboxdomain.DeniedError{Path: "/etc/hosts", Access: sandboxdomain.AccessRead}},
		{"unrelated error", "file not found", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := sandboxdomain.ParseDenial(tt.msg)
			if ok != (tt.want != nil) {
				t.Fatalf("ParseDenial(%q) ok = %v", tt.msg, ok)
			}
			if ok && *got != *tt.want {
				t.Fatalf("ParseDenial(%q) = %+v, want %+v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestGrantsUnlockOnlyWhatApprovalCould(t *testing.T) {
	t.Cleanup(func() {
		Granted = Grants{}
	})

	sandbox := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "file.txt")
	secret := filepath.Join(outside, "prod.env")
	readOnly := filepath.Join(sandbox, "vendor")
	deploy := filepath.Join(sandbox, "deploy")
	token := filepath.Join(deploy, "ci.token")
	for _, p := range []string{target, secret, filepath.Join(readOnly, "lib.go"), filepath.Join(deploy, "run.sh"), token} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, p)
	}

	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Filesystem.Allowed = append([]sandboxdomain.Allowed{{Path: readOnly, Access: sandboxdomain.AccessRead}}, sandboxdomain.Allow(sandbox)...)
	cfg.Tools.Sandbox.Filesystem.Denied = append(cfg.Tools.Sandbox.Filesystem.Denied, sandboxdomain.Denied{Path: deploy, OnViolation: sandboxdomain.ViolationApproval}, sandboxdomain.Denied{Path: "*.token"})

	var denied *sandboxdomain.DeniedError
	if err := ValidateRead(cfg, target); !errors.As(err, &denied) || denied.Rule != "" {
		t.Fatalf("outside allowed must ask, got %v", err)
	}
	if err := ValidateWrite(cfg, filepath.Join(readOnly, "lib.go")); !errors.As(err, &denied) || denied.Access != sandboxdomain.AccessWrite {
		t.Fatalf("a write into a read-only entry must ask, got %v", err)
	}
	if err := ValidateRead(cfg, filepath.Join(readOnly, "lib.go")); err != nil {
		t.Fatalf("a read of a read-only entry is allowed, got %v", err)
	}
	if err := ValidateRead(cfg, filepath.Join(deploy, "run.sh")); !errors.As(err, &denied) || denied.Rule != deploy {
		t.Fatalf("an approval entry must ask and name itself, got %v", err)
	}
	if err := ValidateRead(cfg, secret); err == nil || errors.As(err, &denied) {
		t.Fatalf("a blocking entry must fail without asking, got %v", err)
	}
	if err := ValidateRead(cfg, token); err == nil || errors.As(err, &denied) {
		t.Fatalf("a blocking entry listed after an approval entry must still block, got %v", err)
	}

	Granted.Add(sandboxdomain.Allowed{Path: outside, Access: sandboxdomain.AccessWrite})
	Granted.Add(sandboxdomain.Allowed{Path: deploy, Access: sandboxdomain.AccessRead})
	if err := ValidateWrite(cfg, target); err != nil {
		t.Fatalf("expected access after grant, got %v", err)
	}
	if err := ValidateRead(cfg, filepath.Join(deploy, "run.sh")); err != nil {
		t.Fatalf("expected the approval entry unlocked by its grant, got %v", err)
	}
	if err := ValidateWrite(cfg, filepath.Join(deploy, "run.sh")); err == nil {
		t.Fatal("a read grant must not unlock a write")
	}
	if err := ValidateRead(cfg, secret); err == nil {
		t.Fatal("a granted directory still respects denied")
	}
	Granted.Add(sandboxdomain.Allowed{Path: deploy, Access: sandboxdomain.AccessWrite})
	if err := ValidateRead(cfg, token); err == nil {
		t.Fatal("a grant on an approval entry must not unlock a blocking one beneath it")
	}
	if got := cfg.Tools.Sandbox.Filesystem.Allowed; len(got) != 2 {
		t.Fatalf("grants must not change the configured policy, got %v", got)
	}
}

func TestGrantForScopesConfigDirGrantsToTheFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(t.TempDir())
	cfg := config.DefaultConfig()

	libDir := filepath.Join(t.TempDir(), "lib")
	existingDir := t.TempDir()
	denials := []struct {
		name string
		path string
		rule string
		want string
	}{
		{"a userspace config file grants exactly that file", filepath.Join(home, config.ConfigDirName, "config.yaml"), "", filepath.Join(home, config.ConfigDirName, "config.yaml")},
		{"a project config file grants exactly that file", filepath.Join(config.ConfigDirName, "mcp.yaml"), "", filepath.Join(config.ConfigDirName, "mcp.yaml")},
		{"a denied entry grants exactly that file", filepath.Join(libDir, "dep.go"), "lib/", filepath.Join(libDir, "dep.go")},
		{"any other denial still grants the directory", filepath.Join(libDir, "dep.go"), "", libDir},
		{"an existing directory grants itself", existingDir, "", existingDir},
	}
	for _, tt := range denials {
		t.Run(tt.name, func(t *testing.T) {
			denial := &sandboxdomain.DeniedError{Path: tt.path, Access: sandboxdomain.AccessWrite, Rule: tt.rule}
			got := GrantFor(cfg, denial).Path
			if want, err := filepath.Abs(tt.want); err != nil || got != want {
				t.Fatalf("GrantFor(%s) = %s, want %s", tt.path, got, want)
			}
		})
	}

	denial := &sandboxdomain.DeniedError{Path: filepath.Join(home, config.ConfigDirName, "config.yaml"), Access: sandboxdomain.AccessWrite}
	if got := GrantFor(nil, denial).Path; got != filepath.Join(home, config.ConfigDirName, "config.yaml") {
		t.Fatalf("GrantFor must handle a nil config, got %s", got)
	}
}

func TestValidateWrite_Symlinks(t *testing.T) {
	t.Cleanup(func() {
		Granted = Grants{}
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
	cfg.Tools.Sandbox.Filesystem.Allowed = sandboxdomain.Allow(sandbox)

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
				cfg.Tools.Sandbox.Filesystem.Allowed = sandboxdomain.Allow(tt.dirs...)
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
				Granted.Add(sandboxdomain.Allowed{Path: filepath.Dir(denied.Path), Access: sandboxdomain.AccessWrite})
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

func TestValidateWrite_CustomToolsDirs(t *testing.T) {
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
	cfg.Tools.Sandbox.Filesystem.Allowed = sandboxdomain.Allow(project, filepath.Dir(userTools))
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
