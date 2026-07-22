package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
	observer "go.uber.org/zap/zaptest/observer"
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
	// Disable git's auto-maintenance: git 2.30+ fetch spawns a detached
	// `git maintenance run --auto --detach` that keeps writing to .git/objects
	// after pull returns, racing t.TempDir() cleanup ("unlinkat .git/objects:
	// directory not empty", #950). gc.auto=0 alone doesn't stop it - the detached
	// maintenance run is separate - so disable maintenance.auto too.
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "gc.auto")
	t.Setenv("GIT_CONFIG_VALUE_0", "0")
	t.Setenv("GIT_CONFIG_KEY_1", "maintenance.auto")
	t.Setenv("GIT_CONFIG_VALUE_1", "false")
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

	seedRemote(t, bare, "b.md", "two")

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
	bare := initBareRemote(t)

	memDir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(memDir, "local.md"), "local")

	b := newGitBackend(t, memDir, bare)
	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn: %v", err)
	}
	if !isGitRepo(memDir) {
		t.Fatalf("expected repo initialized in place at %s", memDir)
	}
	requireFile(t, filepath.Join(memDir, "local.md"))

	if err := b.SyncOut(context.Background()); err != nil {
		t.Fatalf("SyncOut: %v", err)
	}
	check := filepath.Join(t.TempDir(), "check")
	mustGit(t, "", "clone", "-b", "main", bare, check)
	requireFile(t, filepath.Join(check, "local.md"))
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

// A sync-in against an unreachable remote logs a Warn that carries git's own
// stderr (e.g. "does not appear to be a git repository") in the "output" field,
// not just the opaque "exit status 128" - so a misconfigured repo is diagnosable
// from the logs.
func TestGitBackend_SyncInFailureSurfacesGitOutput(t *testing.T) {
	isolatedGitEnv(t)

	core, logs := observer.New(zapcore.WarnLevel)
	prev := logger.GetGlobalLogger()
	logger.SetGlobalLogger(zap.New(core))
	defer logger.SetGlobalLogger(prev)

	memDir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(memDir, "fact.md"), "durable")

	b := newGitBackend(t, memDir, filepath.Join(t.TempDir(), "does-not-exist.git"))
	_ = b.SyncIn(context.Background())

	entries := logs.FilterMessage("memory git sync: remote unreachable, skipping sync-in").All()
	if len(entries) != 1 {
		t.Fatalf("expected one 'remote unreachable' warning, got %d", len(entries))
	}
	out, _ := entries[0].ContextMap()["output"].(string)
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected the warning to surface git's stderr in the 'output' field, but it was empty")
	}
}

// When memory.backend.git.repo changes after the memory dir was already
// initialized (e.g. the user switches ssh -> https), sync-out reconciles the
// origin remote to the new URL and pushes there, instead of silently pushing to
// the URL captured at first init.
func TestGitBackend_ReconcilesOriginOnRepoChange(t *testing.T) {
	isolatedGitEnv(t)
	oldRemote := initBareRemote(t)
	seedRemote(t, oldRemote, "seed.md", "seed")
	newRemote := initBareRemote(t)

	memDir := filepath.Join(t.TempDir(), "memory")
	mustGit(t, "", "clone", "-b", "main", oldRemote, memDir)

	b := newGitBackend(t, memDir, newRemote)
	writeFile(t, filepath.Join(memDir, "fact.md"), "hello")
	if err := b.SyncOut(context.Background()); err != nil {
		t.Fatalf("SyncOut: %v", err)
	}

	got := strings.TrimSpace(mustGit(t, memDir, "config", "--get", "remote.origin.url"))
	if got != newRemote {
		t.Fatalf("origin = %q, want reconciled to %q", got, newRemote)
	}
	check := filepath.Join(t.TempDir(), "check")
	mustGit(t, "", "clone", "-b", "main", newRemote, check)
	requireFile(t, filepath.Join(check, "fact.md"))
}

// When the memory dir is already a git repo with local commits but the remote
// branch does not exist yet (fresh/empty remote), sync-in must not fail on the
// missing ref: it seeds the remote, creating the branch and pushing the local
// memory.
func TestGitBackend_SyncInSeedsMissingRemoteBranch(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t) // empty: no main branch yet

	memDir := filepath.Join(t.TempDir(), "memory")
	mustGit(t, "", "init", "-b", "main", memDir)
	mustGit(t, memDir, "remote", "add", "origin", bare)
	writeFile(t, filepath.Join(memDir, "MEMORY.md"), "# Memory Index\n")
	mustGit(t, memDir, "add", "-A")
	mustGit(t, memDir, "commit", "-m", "local memory")

	b := newGitBackend(t, memDir, bare)
	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn should seed a missing remote branch, not error: %v", err)
	}

	check := filepath.Join(t.TempDir(), "check")
	mustGit(t, "", "clone", "-b", "main", bare, check)
	requireFile(t, filepath.Join(check, "MEMORY.md"))
}

// A fresh (non-git) memory dir against an empty remote is initialized in place
// and seeded on sync-in: the branch is created and the local files pushed,
// without waiting for a later memory write.
func TestGitBackend_SyncInSeedsFreshDirEmptyRemote(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t)

	memDir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(memDir, "MEMORY.md"), "# Memory Index\n")

	b := newGitBackend(t, memDir, bare)
	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn: %v", err)
	}

	check := filepath.Join(t.TempDir(), "check")
	mustGit(t, "", "clone", "-b", "main", bare, check)
	requireFile(t, filepath.Join(check, "MEMORY.md"))
}

func TestGitBackend_FailureDegrades(t *testing.T) {
	isolatedGitEnv(t)
	memDir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(memDir, "fact.md"), "durable")

	b := newGitBackend(t, memDir, filepath.Join(t.TempDir(), "does-not-exist.git"))

	_ = b.SyncIn(context.Background())
	_ = b.SyncOut(context.Background())

	requireFile(t, filepath.Join(memDir, "fact.md"))
}

// clearGitIdentity strips any author identity (config + GIT_* env) so a bare
// `git commit` would fail - the state of a fresh container or CI runner. Call it
// AFTER any test setup that needs to commit (seedRemote/clone).
func clearGitIdentity(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "no-such-gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "") // register cleanup to restore the original value...
		_ = os.Unsetenv(k)
	}
}

// In an identity-less environment the backend must supply a fallback commit
// identity, otherwise `git commit` fails ("Please tell me who you are") and
// memory silently never syncs out. A configured identity is preserved elsewhere;
// here there is none.
func TestGitBackend_CommitsWithoutAmbientIdentity(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t)
	seedRemote(t, bare, "seed.md", "seed")

	memDir := filepath.Join(t.TempDir(), "memory")
	mustGit(t, "", "clone", "-b", "main", bare, memDir)
	writeFile(t, filepath.Join(memDir, "fact.md"), "hello")

	clearGitIdentity(t)

	b := newGitBackend(t, memDir, bare)
	if err := b.SyncOut(context.Background()); err != nil {
		t.Fatalf("SyncOut must commit with a fallback identity in an identity-less env: %v", err)
	}

	check := filepath.Join(t.TempDir(), "check")
	mustGit(t, "", "clone", "-b", "main", bare, check)
	requireFile(t, filepath.Join(check, "fact.md"))
}

// A user with pre-existing local memory (a non-git dir) who points the backend
// at an already-populated remote must have sync-in ADOPT that remote's history
// and union the files - not initialize an unrelated-history repo that can never
// push or pull. Conflicting files resolve to the local copy (-X ours).
func TestGitBackend_SyncInAdoptsPopulatedRemote(t *testing.T) {
	isolatedGitEnv(t)
	bare := initBareRemote(t)
	seedRemote(t, bare, "remote.md", "from-remote")
	seedRemote(t, bare, "MEMORY.md", "# remote index\n") // conflicts with local below

	memDir := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(memDir, "local.md"), "from-local")
	writeFile(t, filepath.Join(memDir, "MEMORY.md"), "# local index\n")

	b := newGitBackend(t, memDir, bare)
	if err := b.SyncIn(context.Background()); err != nil {
		t.Fatalf("SyncIn should adopt the populated remote, not error: %v", err)
	}
	if !isGitRepo(memDir) {
		t.Fatalf("expected repo initialized in place at %s", memDir)
	}
	requireFile(t, filepath.Join(memDir, "remote.md")) // remote-only file adopted
	requireFile(t, filepath.Join(memDir, "local.md"))  // local file preserved
	if got, _ := os.ReadFile(filepath.Join(memDir, "MEMORY.md")); !strings.Contains(string(got), "local index") {
		t.Errorf("expected local MEMORY.md to win the conflict, got:\n%s", got)
	}

	// Adopt pushes the union, so a fresh clone sees both contributions and a later
	// sync-out is a clean no-op (no unrelated-history failure).
	if err := b.SyncOut(context.Background()); err != nil {
		t.Fatalf("SyncOut after adopt: %v", err)
	}
	check := filepath.Join(t.TempDir(), "check")
	mustGit(t, "", "clone", "-b", "main", bare, check)
	requireFile(t, filepath.Join(check, "remote.md"))
	requireFile(t, filepath.Join(check, "local.md"))
}
