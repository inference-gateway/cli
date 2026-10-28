package infrastructure

import (
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
