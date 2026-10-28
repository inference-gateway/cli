package infrastructure

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		if _, err := utils.RunGit(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	write(t, dir, "README.md", "hello\n")
	write(t, dir, ".gitignore", "ignored.log\n")
	for _, args := range [][]string{{"add", "."}, {"commit", "--quiet", "-m", "init"}} {
		if _, err := utils.RunGit(ctx, dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWorkspaceRoundTrip(t *testing.T) {
	src := gitRepo(t)
	write(t, src, "README.md", "hello\nchanged\n")
	write(t, src, "notes/todo.txt", "untracked\n")
	write(t, src, "ignored.log", "noise\n")
	write(t, src, ".env", "SECRET=1\n")
	write(t, src, "build/out.bin", "artifact\n")
	denied := func(abs string) bool { return strings.HasSuffix(abs, ".env") }

	ws, err := PackWorkspace(t.Context(), src, []string{"build/out.bin"}, denied)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if ws.Branch != "main" || len(ws.Bundle) == 0 || !strings.Contains(string(ws.Patch), "+changed") {
		t.Fatalf("workspace = branch %q, bundle %d bytes, patch %q", ws.Branch, len(ws.Bundle), ws.Patch)
	}

	dst := filepath.Join(t.TempDir(), "restored")
	if err := UnpackWorkspace(t.Context(), ws, dst); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if got := read(t, dst, "README.md"); got != "hello\nchanged\n" {
		t.Fatalf("README.md = %q", got)
	}
	if got := read(t, dst, "notes/todo.txt"); got != "untracked\n" {
		t.Fatalf("untracked file = %q", got)
	}
	if got := read(t, dst, "build/out.bin"); got != "artifact\n" {
		t.Fatalf("extra file = %q", got)
	}
	for _, absent := range []string{".env", "ignored.log"} {
		if _, err := os.Stat(filepath.Join(dst, absent)); err == nil {
			t.Fatalf("%s must not travel", absent)
		}
	}
	branch, err := utils.RunGit(t.Context(), dst, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(string(branch)) != "main" {
		t.Fatalf("restored branch = %q, %v", branch, err)
	}
	if err := UnpackWorkspace(t.Context(), ws, dst); err == nil {
		t.Fatal("unpacking over an existing workspace must fail")
	}
}

func TestWorkspaceDetachedHeadAndRefusals(t *testing.T) {
	src := gitRepo(t)
	sha, _ := utils.RunGit(t.Context(), src, "rev-parse", "HEAD")
	if _, err := utils.RunGit(t.Context(), src, "checkout", "--quiet", "--detach"); err != nil {
		t.Fatal(err)
	}
	ws, err := PackWorkspace(t.Context(), src, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Branch != strings.TrimSpace(string(sha)) {
		t.Fatalf("a detached head packs its commit, got %q", ws.Branch)
	}
	dst := filepath.Join(t.TempDir(), "restored")
	if err := UnpackWorkspace(t.Context(), ws, dst); err != nil {
		t.Fatalf("Unpack detached: %v", err)
	}

	if _, err := PackWorkspace(t.Context(), src, []string{"../outside"}, nil); err == nil {
		t.Fatal("an extra file outside the project must be refused")
	}
	if _, err := PackWorkspace(t.Context(), t.TempDir(), nil, nil); err == nil {
		t.Fatal("a directory without a git commit must be refused")
	}

	write(t, src, "README.md", "diverged\n")
	ws, _ = PackWorkspace(t.Context(), src, nil, nil)
	ws.Bundle = nil
	if err := UnpackWorkspace(t.Context(), ws, filepath.Join(t.TempDir(), "broken")); err == nil {
		t.Fatal("an empty bundle must fail the restore")
	}
}

func TestPackWorkspaceKeepsExtraFilesInsideTheProject(t *testing.T) {
	src := gitRepo(t)
	outside := t.TempDir()
	write(t, outside, "secret", "do not ship\n")
	if err := os.Symlink(outside, filepath.Join(src, "linked")); err != nil {
		t.Fatal(err)
	}
	write(t, src, "local.env", "TOKEN=x\n")
	denyEnv := func(abs string) bool { return strings.HasSuffix(abs, ".env") }

	if _, err := PackWorkspace(t.Context(), src, []string{"linked/secret"}, nil); err == nil {
		t.Fatal("an extra file behind a symlinked directory outside the project must be refused")
	}
	if _, err := PackWorkspace(t.Context(), src, []string{"local.env"}, denyEnv); err == nil || !strings.Contains(err.Error(), "sandbox policy") {
		t.Fatalf("an extra file the sandbox denies must be refused, got %v", err)
	}
}

func TestUntarRefusesEntriesThatLeaveTheWorkspace(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{"parent path", "../evil"},
		{"absolute path", "/evil"},
		{"through a symlinked directory", "linked/evil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			outside := t.TempDir()
			if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
				t.Fatal(err)
			}
			if err := untar(tarOf(t, tt.entry, "pwned\n"), dir); err == nil {
				t.Fatalf("entry %q was written", tt.entry)
			}
			if entries, _ := os.ReadDir(outside); len(entries) != 0 {
				t.Fatalf("entry %q landed outside the workspace", tt.entry)
			}
		})
	}
}

func tarOf(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
