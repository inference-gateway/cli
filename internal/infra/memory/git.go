package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// GitBackend syncs the memory directory with a git remote. It shells out to the
// git CLI (mirroring the exec.Command("git", ...) style in internal/services/
// gitdiff) and inherits the ambient environment unchanged, so auth uses the
// user's default git/ssh config (ssh-agent, credential helper, GIT_* env). Every
// command runs under a per-operation timeout so a misconfigured remote ends the
// command instead of hanging on a credential prompt.
//
// All operations are best-effort: they return an error for tests/telemetry, but
// callers log and continue - a sync failure never aborts the agent run.
type GitBackend struct {
	cfg    *config.Config
	inOnce sync.Once
}

// NewGitBackend constructs a git-backed memory sync. It warns once if the repo
// URL embeds credentials (they persist in .git/config) - ssh or a credential
// helper is preferred.
func NewGitBackend(cfg *config.Config) *GitBackend {
	if repoEmbedsCredentials(cfg.Memory.Backend.Git.Repo) {
		logger.Warn("memory git backend: repo URL embeds credentials, which persist in .git/config; prefer ssh or a git credential helper",
			"repo", redactRepo(cfg.Memory.Backend.Git.Repo))
	}
	return &GitBackend{cfg: cfg}
}

func (b *GitBackend) git() config.MemoryGitConfig { return b.cfg.Memory.Backend.Git }

// SyncIn pulls the memory directory from the remote, cloning it (or initializing
// a repo in place) on first run. It runs at most once per process and only when
// on_start is "pull".
func (b *GitBackend) SyncIn(ctx context.Context) error {
	if !b.git().PullOnStart() {
		return nil
	}
	var err error
	b.inOnce.Do(func() { err = b.syncIn(ctx) })
	return err
}

func (b *GitBackend) syncIn(ctx context.Context) error {
	dir, err := b.cfg.ResolveMemoryDir()
	if err != nil {
		logger.Warn("memory git sync: resolve dir failed", "error", err)
		return err
	}
	unlock := lockDir(dir)
	defer unlock()

	g := b.git()
	branch := g.EffectiveBranch()

	hasBranch, out, err := b.remoteHasBranch(ctx, g.Repo, branch)
	if err != nil {
		logger.Warn("memory git sync: remote unreachable, skipping sync-in",
			"repo", redactRepo(g.Repo), "error", err, "output", trim(out))
		return err
	}

	if isGitRepo(dir) {
		return b.syncInExisting(ctx, dir, branch, hasBranch)
	}
	return b.syncInFresh(ctx, dir, branch, hasBranch)
}

// syncInExisting reconciles the origin remote, then pulls when the remote branch
// exists. When it does not (a fresh/empty remote), it seeds the remote from
// local memory - creating the branch and pushing - instead of failing on the
// missing remote ref.
func (b *GitBackend) syncInExisting(ctx context.Context, dir, branch string, remoteHasBranch bool) error {
	if err := b.ensureRepo(ctx, dir); err != nil {
		return err
	}
	if !remoteHasBranch {
		return b.stageCommitPush(ctx, dir, branch, true)
	}
	if out, err := b.run(ctx, dir, "pull", "--rebase", "--autostash", "origin", branch); err != nil {
		logger.Warn("memory git sync: pull failed", "error", err, "output", trim(out))
		return err
	}
	return nil
}

// syncInFresh clones when the remote already has the branch and the dir is
// empty; otherwise it initializes a repo in place and either seeds a missing
// remote branch from local memory, or - when local memory already exists and the
// remote branch is populated - adopts the remote history into it (see
// adoptRemoteBranch) rather than leaving an unrelated-history repo.
func (b *GitBackend) syncInFresh(ctx context.Context, dir, branch string, remoteHasBranch bool) error {
	g := b.git()
	if remoteHasBranch && isEmptyOrMissing(dir) {
		if out, err := b.run(ctx, "", "clone", "--branch", branch, "--single-branch", g.Repo, dir); err != nil {
			logger.Warn("memory git sync: clone failed", "repo", redactRepo(g.Repo), "error", err, "output", trim(out))
			return err
		}
		return nil
	}
	if err := b.ensureRepo(ctx, dir); err != nil {
		return err
	}
	if !remoteHasBranch {
		return b.stageCommitPush(ctx, dir, branch, true)
	}
	return b.adoptRemoteBranch(ctx, dir, branch)
}

// adoptRemoteBranch unions pre-existing local memory with an already-populated
// remote branch. ensureRepo has just init'd an in-place repo whose history is
// unrelated to the remote's, so a plain pull --rebase would fail forever on the
// unrelated histories; instead this commits the local files, fetches the remote
// branch, and merges it with --allow-unrelated-histories so later push/pull
// reconcile normally. Conflicting files resolve to the local copy (-X ours),
// matching the backend's last-writer-wins posture; the merge still adopts every
// remote-only fact file. (One known gap, in line with #683's out-of-scope
// conflict-resolution: a conflicting MEMORY.md keeps the local index, so
// remote-only facts may be present as files but not listed until the next write.)
// Best-effort: on failure it aborts the merge, returns the error, and the caller
// logs and continues.
func (b *GitBackend) adoptRemoteBranch(ctx context.Context, dir, branch string) error {
	if out, err := b.run(ctx, dir, "add", "-A"); err != nil {
		logger.Warn("memory git sync: adopt add failed", "error", err, "output", trim(out))
		return err
	}
	status, err := b.run(ctx, dir, "status", "--porcelain")
	if err != nil {
		logger.Warn("memory git sync: adopt status failed", "error", err, "output", trim(status))
		return err
	}
	if len(strings.TrimSpace(string(status))) > 0 {
		if err := b.commit(ctx, dir); err != nil {
			return err
		}
	}
	if out, err := b.run(ctx, dir, "fetch", "origin", branch); err != nil {
		logger.Warn("memory git sync: adopt fetch failed", "error", err, "output", trim(out))
		return err
	}
	if out, err := b.run(ctx, dir, "merge", "--allow-unrelated-histories", "-X", "ours", "--no-edit", "FETCH_HEAD"); err != nil {
		logger.Warn("memory git sync: adopt merge failed", "error", err, "output", trim(out))
		_, _ = b.run(ctx, dir, "merge", "--abort")
		return err
	}
	// The merge commit is ahead of the remote but the tree is clean, so a later
	// stageCommitPush would no-op; push the union now (like the seed path) so the
	// local contribution reaches the remote instead of waiting for the next write.
	return b.pushWithRetry(ctx, dir, branch)
}

// SyncOut commits and pushes the memory directory, but only when it has changes.
// It is safe to call repeatedly (the git status check gates the commit).
func (b *GitBackend) SyncOut(ctx context.Context) error {
	if !b.git().PushOnFinish() {
		return nil
	}
	dir, err := b.cfg.ResolveMemoryDir()
	if err != nil {
		logger.Warn("memory git sync: resolve dir failed", "error", err)
		return err
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	unlock := lockDir(dir)
	defer unlock()

	if err := b.ensureRepo(ctx, dir); err != nil {
		return err
	}
	return b.stageCommitPush(ctx, dir, b.git().EffectiveBranch(), false)
}

// stageCommitPush stages all memory changes, commits when the tree is dirty, and
// pushes. pushWhenClean forces a push even with nothing new to commit - used to
// seed a remote whose branch does not exist yet from existing local commits;
// sync-out passes false so an unchanged memory is a no-op. Pushing to a missing
// remote branch creates it. With no local commits at all there is nothing to
// push, so it is a no-op regardless.
func (b *GitBackend) stageCommitPush(ctx context.Context, dir, branch string, pushWhenClean bool) error {
	if out, err := b.run(ctx, dir, "add", "-A"); err != nil {
		logger.Warn("memory git sync: add failed", "error", err, "output", trim(out))
		return err
	}
	status, err := b.run(ctx, dir, "status", "--porcelain")
	if err != nil {
		logger.Warn("memory git sync: status failed", "error", err, "output", trim(status))
		return err
	}
	dirty := len(strings.TrimSpace(string(status))) > 0
	if dirty {
		if err := b.commit(ctx, dir); err != nil {
			return err
		}
	}
	if !dirty && !pushWhenClean {
		return nil
	}
	if !b.hasCommits(ctx, dir) {
		return nil
	}
	return b.pushWithRetry(ctx, dir, branch)
}

// hasCommits reports whether the repo has at least one commit (HEAD resolves).
func (b *GitBackend) hasCommits(ctx context.Context, dir string) bool {
	_, err := b.run(ctx, dir, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// Fallback commit identity, applied only for whichever of user.name/user.email
// the ambient git config leaves unset. Without this a bare `git commit` fails
// ("Please tell me who you are") in an identity-less environment - a fresh
// container or CI runner, exactly where the channels/scheduler/heartbeat daemon
// runs - and memory would silently never sync out.
const (
	fallbackCommitName  = "infer"
	fallbackCommitEmail = "infer@localhost"
)

// commit records the staged memory changes, supplying the fallback identity for
// any missing field so the commit succeeds without overriding a configured
// user.name/user.email.
func (b *GitBackend) commit(ctx context.Context, dir string) error {
	args := append(b.identityArgs(ctx, dir), "commit", "-m", b.git().EffectiveCommitMessage())
	if out, err := b.run(ctx, dir, args...); err != nil {
		logger.Warn("memory git sync: commit failed", "error", err, "output", trim(out))
		return err
	}
	return nil
}

// identityArgs returns leading `-c user.name=... / -c user.email=...` overrides
// for only the identity fields git currently resolves no value for, so a real
// configured identity is preserved while an identity-less environment still gets
// a usable committer.
func (b *GitBackend) identityArgs(ctx context.Context, dir string) []string {
	var args []string
	if b.gitConfigMissing(ctx, dir, "user.name") {
		args = append(args, "-c", "user.name="+fallbackCommitName)
	}
	if b.gitConfigMissing(ctx, dir, "user.email") {
		args = append(args, "-c", "user.email="+fallbackCommitEmail)
	}
	return args
}

// gitConfigMissing reports whether git resolves no value for key in dir.
func (b *GitBackend) gitConfigMissing(ctx context.Context, dir, key string) bool {
	out, err := b.run(ctx, dir, "config", "--get", key)
	return err != nil || strings.TrimSpace(string(out)) == ""
}

// maxPushAttempts bounds the push / pull-rebase retry loop that reconciles with
// concurrent pushes to the same memory remote (overlapping headless runs).
const maxPushAttempts = 3

// pushWithRetry pushes, and whenever the push is rejected because another run
// pushed to the memory remote, it pulls with rebase - replaying the local commit
// onto the new remote tip - and retries, up to maxPushAttempts times. This keeps
// concurrent memory writers from clobbering each other. A failed rebase is
// aborted so the working copy is left clean.
func (b *GitBackend) pushWithRetry(ctx context.Context, dir, branch string) error {
	var lastErr error
	for attempt := 1; attempt <= maxPushAttempts; attempt++ {
		_, err := b.run(ctx, dir, "push", "-u", "origin", branch)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == maxPushAttempts {
			break
		}
		if rout, rerr := b.run(ctx, dir, "pull", "--rebase", "--autostash", "origin", branch); rerr != nil {
			logger.Warn("memory git sync: rebase onto remote failed", "error", rerr, "output", trim(rout))
			_, _ = b.run(ctx, dir, "rebase", "--abort")
			return rerr
		}
	}
	logger.Warn("memory git sync: push failed after rebase retries", "attempts", maxPushAttempts, "error", lastErr)
	return lastErr
}

// ensureRepo makes dir a git repo on the configured branch with the origin
// remote set, initializing it in place if needed (idempotent).
func (b *GitBackend) ensureRepo(ctx context.Context, dir string) error {
	g := b.git()
	if isGitRepo(dir) {
		return b.ensureOrigin(ctx, dir, g.Repo)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn("memory git sync: mkdir failed", "dir", dir, "error", err)
		return err
	}
	steps := [][]string{
		{"init"},
		{"remote", "add", "origin", g.Repo},
		{"checkout", "-B", g.EffectiveBranch()},
	}
	for _, args := range steps {
		if out, err := b.run(ctx, dir, args...); err != nil {
			logger.Warn("memory git sync: repo init failed", "step", args[0], "error", err, "output", trim(out))
			return err
		}
	}
	return nil
}

// ensureOrigin points the origin remote at repo, adding it when missing and
// updating it when the configured URL changed. Without this, an already-
// initialized memory dir keeps pushing to the URL captured at first init, so
// switching memory.backend.git.repo (e.g. ssh -> https) would be silently
// ignored. config --get is empty (not an error line) when origin is unset.
func (b *GitBackend) ensureOrigin(ctx context.Context, dir, repo string) error {
	cur, _ := b.run(ctx, dir, "config", "--get", "remote.origin.url")
	current := strings.TrimSpace(string(cur))
	if current == repo {
		return nil
	}
	verb := "set-url"
	if current == "" {
		verb = "add"
	}
	if out, err := b.run(ctx, dir, "remote", verb, "origin", repo); err != nil {
		logger.Warn("memory git sync: failed to set origin remote", "error", err, "output", trim(out))
		return err
	}
	return nil
}

// remoteHasBranch reports whether the remote already has branch, via ls-remote.
// A clean exit with empty output means the remote is reachable but empty (or
// lacks the branch); a non-zero exit means unreachable/unauthorized. The raw
// command output is returned so the caller can surface git's stderr (e.g.
// "Repository not found") instead of the opaque "exit status 128".
func (b *GitBackend) remoteHasBranch(ctx context.Context, repo, branch string) (bool, []byte, error) {
	out, err := b.run(ctx, "", "ls-remote", "--heads", repo, branch)
	if err != nil {
		return false, out, err
	}
	return len(strings.TrimSpace(string(out))) > 0, out, nil
}

// run executes a git command under the per-op timeout. It does NOT set cmd.Env,
// so the process inherits the ambient environment (the user's default git/ssh
// config and credential chain). workdir is the working directory ("" = inherit).
func (b *GitBackend) run(ctx context.Context, workdir string, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, b.git().EffectiveTimeout())
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	return cmd.CombinedOutput()
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

func isEmptyOrMissing(dir string) bool {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		return false
	}
	return len(entries) == 0
}

func trim(out []byte) string {
	const max = 2000
	s := strings.TrimSpace(string(out))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// repoEmbedsCredentials reports whether an http(s) repo URL carries userinfo
// (e.g. https://user:token@host/...). scp-style ssh URLs (git@host:path) and
// ssh:// URLs are not flagged - the "git" there is a username, not a secret.
func repoEmbedsCredentials(repo string) bool {
	i := strings.Index(repo, "://")
	if i < 0 {
		return false
	}
	scheme := repo[:i]
	if scheme != "http" && scheme != "https" {
		return false
	}
	authority := repo[i+3:]
	if slash := strings.IndexByte(authority, '/'); slash >= 0 {
		authority = authority[:slash]
	}
	return strings.IndexByte(authority, '@') >= 0
}

// redactRepo masks userinfo in an http(s) URL for logging.
func redactRepo(repo string) string {
	if !repoEmbedsCredentials(repo) {
		return repo
	}
	i := strings.Index(repo, "://")
	rest := repo[i+3:]
	at := strings.IndexByte(rest, '@')
	return repo[:i+3] + "***@" + rest[at+1:]
}
