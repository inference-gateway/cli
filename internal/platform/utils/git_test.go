package utils

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// newGitRepo initialises a throwaway repository with one commit.
func newGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed; skipping git-backed test")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestRunGit(t *testing.T) {
	repo := newGitRepo(t)

	out, err := RunGit(context.Background(), repo, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		t.Fatalf("RunGit: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "true" {
		t.Errorf("RunGit rev-parse = %q, want %q", got, "true")
	}

	if _, err := RunGit(context.Background(), t.TempDir(), "rev-parse", "--is-inside-work-tree"); err == nil {
		t.Errorf("RunGit(non-repo) err = nil, want error")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunGit(ctx, repo, "log"); err == nil {
		t.Errorf("RunGit(cancelled ctx) err = nil, want error")
	}
}
