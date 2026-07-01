package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// isolatedGitEnv points git at empty global/system config and a fixed identity
// so tests don't depend on (or mutate) the developer's git config (gpg signing,
// user.name, etc.) and the backend's inherited-env commits succeed in CI.
func isolatedGitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "no-such-gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "infer-test")
	t.Setenv("GIT_AUTHOR_EMAIL", "infer-test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "infer-test")
	t.Setenv("GIT_COMMITTER_EMAIL", "infer-test@example.com")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v (dir=%q) failed: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

func initBareRemote(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit(t, "", "init", "--bare", "-b", "main", bare)
	return bare
}

// seedRemote commits name=content on main of the bare remote.
func seedRemote(t *testing.T, bare, name, content string) {
	t.Helper()
	work := t.TempDir()
	mustGit(t, "", "clone", bare, work)
	mustGit(t, work, "checkout", "-B", "main")
	writeFile(t, filepath.Join(work, name), content)
	mustGit(t, work, "add", "-A")
	mustGit(t, work, "commit", "-m", "seed "+name)
	mustGit(t, work, "push", "-u", "origin", "main")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func requireFile(t *testing.T, path string) {
	t.Helper()
	if !fileExists(path) {
		t.Fatalf("expected file to exist: %s", path)
	}
}

func newGitBackend(t *testing.T, memDir, repo string) *GitBackend {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.Dir = memDir
	cfg.Memory.MaxChars = config.DefaultMemoryMaxChars
	cfg.Memory.Backend.Type = config.MemoryBackendGit
	cfg.Memory.Backend.Git.Repo = repo
	cfg.Memory.Backend.Git.Branch = "main"
	cfg.Memory.Backend.Git.Timeout = config.DefaultMemoryGitTimeout
	return NewGitBackend(cfg)
}

func TestGitBackend_CloneOnMissing(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t)
	seedRemote(t, bare, "MEMORY.md", "# Memory Index\n")

	memDir := filepath.Join(t.TempDir(), "memory")
	b := newGitBackend(t, memDir, bare)

	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn: %v", err)
	}
	if !isGitRepo(memDir) {
		t.Fatalf("expected %s to be a git repo after clone", memDir)
	}
	requireFile(t, filepath.Join(memDir, "MEMORY.md"))
}

func TestGitBackend_PullOnPresent(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t)
	seedRemote(t, bare, "a.md", "one")

	memDir := filepath.Join(t.TempDir(), "memory")
	mustGit(t, "", "clone", "-b", "main", bare, memDir)

	seedRemote(t, bare, "b.md", "two") // remote advances after the local clone

	b := newGitBackend(t, memDir, bare)
	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn: %v", err)
	}
	requireFile(t, filepath.Join(memDir, "b.md"))
}

func TestGitBackend_PushWhenDirty(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t)
	seedRemote(t, bare, "seed.md", "seed")

	memDir := filepath.Join(t.TempDir(), "memory")
	mustGit(t, "", "clone", "-b", "main", bare, memDir)
	writeFile(t, filepath.Join(memDir, "fact.md"), "hello")

	b := newGitBackend(t, memDir, bare)
	if err := b.SyncOut(context.Background()); err != nil {
		t.Fatalf("SyncOut: %v", err)
	}

	check := filepath.Join(t.TempDir(), "check")
	mustGit(t, "", "clone", "-b", "main", bare, check)
	requireFile(t, filepath.Join(check, "fact.md"))
}

func TestGitBackend_PushRebasesOnConcurrentPush(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t)
	seedRemote(t, bare, "seed.md", "seed")

	memDir := filepath.Join(t.TempDir(), "memory")
	mustGit(t, "", "clone", "-b", "main", bare, memDir)

	seedRemote(t, bare, "other.md", "from-other")

	writeFile(t, filepath.Join(memDir, "mine.md"), "from-me")

	b := newGitBackend(t, memDir, bare)
	if err := b.SyncOut(context.Background()); err != nil {
		t.Fatalf("SyncOut: %v", err)
	}

	check := filepath.Join(t.TempDir(), "check")
	mustGit(t, "", "clone", "-b", "main", bare, check)
	requireFile(t, filepath.Join(check, "mine.md"))
	requireFile(t, filepath.Join(check, "other.md"))
}

func TestGitBackend_PushNoopWhenClean(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t)
	seedRemote(t, bare, "seed.md", "seed")

	memDir := filepath.Join(t.TempDir(), "memory")
	mustGit(t, "", "clone", "-b", "main", bare, memDir)

	before := mustGit(t, bare, "rev-parse", "main")
	b := newGitBackend(t, memDir, bare)
	if err := b.SyncOut(context.Background()); err != nil {
		t.Fatalf("SyncOut: %v", err)
	}
	after := mustGit(t, bare, "rev-parse", "main")
	if before != after {
		t.Fatalf("expected no new commit on a clean repo; before=%q after=%q", before, after)
	}
}

func TestGitBackend_AdoptEmptyRemote(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t) // empty: no commits, no branch

	memDir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(memDir, "local.md"), "local") // pre-existing non-git memory

	b := newGitBackend(t, memDir, bare)
	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn: %v", err)
	}
	if !isGitRepo(memDir) {
		t.Fatalf("expected repo initialized in place at %s", memDir)
	}
	requireFile(t, filepath.Join(memDir, "local.md")) // local content preserved

	if err := b.SyncOut(context.Background()); err != nil {
		t.Fatalf("SyncOut: %v", err)
	}
	check := filepath.Join(t.TempDir(), "check")
	mustGit(t, "", "clone", "-b", "main", bare, check)
	requireFile(t, filepath.Join(check, "local.md")) // local pushed up
}

func TestGitBackend_SyncInRunsOnce(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t)
	seedRemote(t, bare, "a.md", "one")

	memDir := filepath.Join(t.TempDir(), "memory")
	b := newGitBackend(t, memDir, bare)
	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn 1: %v", err)
	}
	seedRemote(t, bare, "b.md", "two")
	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn 2: %v", err)
	}
	if fileExists(filepath.Join(memDir, "b.md")) {
		t.Fatal("expected SyncIn to run at most once per process; the second call should not have pulled")
	}
}

func TestGitBackend_FailureDegrades(t *testing.T) {
	isolatedGitEnv(t)
	memDir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(memDir, "fact.md"), "durable")

	// Repo points at a path that does not exist: sync must not panic and must
	// leave local memory intact.
	b := newGitBackend(t, memDir, filepath.Join(t.TempDir(), "does-not-exist.git"))

	_ = b.SyncIn(context.Background())
	_ = b.SyncOut(context.Background())

	requireFile(t, filepath.Join(memDir, "fact.md"))
}
