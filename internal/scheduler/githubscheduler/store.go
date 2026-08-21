package githubscheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

const (
	// DefaultRepoName is the repository created under the authenticated user
	// when scheduler.github.repository is not configured.
	DefaultRepoName = ".routines"

	ghCommandTimeout = 30 * time.Second
	ghNetworkTimeout = 2 * time.Minute
)

// Store decorates a local ScheduledJobStorage: reads delegate to the local
// store (source of truth for the CLI), writes additionally render the job's
// Actions workflow and deploy it to the configured GitHub repository - pushed
// straight to the default branch, or via a PR when pull_requests is enabled.
// A failed GitHub sync aborts the local write, so no phantom jobs.
type Store struct {
	inner        storage.ScheduledJobStorage
	runner       CommandRunner
	cfg          config.SchedulerGitHubConfig
	defaultModel string

	mu        sync.Mutex // one clone/branch/PR sequence at a time
	lastPRURL string
}

// NewStore wraps inner with GitHub workflow sync. runner is typically
// &githubsetup.RealRunner{}.
func NewStore(inner storage.ScheduledJobStorage, runner CommandRunner, cfg config.SchedulerGitHubConfig, defaultModel string) *Store {
	return &Store{inner: inner, runner: runner, cfg: cfg, defaultModel: defaultModel}
}

// LastPRURL returns the URL of the PR opened by the most recent successful
// SaveJob/DeleteJob, or "" when the last write needed no PR. The schedule tool
// type-asserts for this to surface the link to the user.
func (s *Store) LastPRURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastPRURL
}

// LoadJob delegates to the local store.
func (s *Store) LoadJob(ctx context.Context, id string) (*scheddomain.ScheduledJob, error) {
	return s.inner.LoadJob(ctx, id)
}

// ListJobs delegates to the local store.
func (s *Store) ListJobs(ctx context.Context) ([]*scheddomain.ScheduledJob, error) {
	return s.inner.ListJobs(ctx)
}

// SaveJob renders the job's workflow, deploys it to the repo, then persists
// the job locally. Validation failures and gh errors abort before the local save.
func (s *Store) SaveJob(ctx context.Context, job *scheddomain.ScheduledJob) error {
	ghCron, err := TranslateCron(job.CronExpression)
	if err != nil {
		return err
	}
	content, err := RenderWorkflow(job, ghCron, s.defaultModel, s.cfg)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	prURL, err := s.syncRepo(ctx, job, func(dir string) (bool, error) {
		path := filepath.Join(dir, WorkflowPath(job.ID))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return false, err
		}
		return true, os.WriteFile(path, content, 0644)
	}, "upsert")
	if err != nil {
		return err
	}
	s.lastPRURL = prURL
	return s.inner.SaveJob(ctx, job)
}

// DeleteJob removes the job's workflow from the repo (skipped when the file
// is absent), then deletes the job locally.
func (s *Store) DeleteJob(ctx context.Context, id string) error {
	job, err := s.inner.LoadJob(ctx, id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	prURL, err := s.syncRepo(ctx, job, func(dir string) (bool, error) {
		path := filepath.Join(dir, WorkflowPath(id))
		if _, err := os.Stat(path); err != nil {
			return false, nil // never deployed; nothing to remove
		}
		return true, os.Remove(path)
	}, "remove")
	if err != nil {
		return err
	}
	s.lastPRURL = prURL
	return s.inner.DeleteJob(ctx, id)
}

// syncRepo ensures the repo exists, clones it, applies mutate in the checkout,
// commits, and deploys the change: pushed straight to the default branch, or -
// when pull_requests is configured - via a branch and PR (URL returned). mutate
// returns false to signal a no-op (nothing is pushed and "" is returned).
func (s *Store) syncRepo(ctx context.Context, job *scheddomain.ScheduledJob, mutate func(dir string) (bool, error), verb string) (string, error) {
	repo, err := ResolveRepo(ctx, s.runner, s.cfg.Repository)
	if err != nil {
		return "", err
	}
	if err := s.ensureRepo(ctx, repo); err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "infer-routines-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if out, err := s.run(ctx, ghNetworkTimeout, "gh", "repo", "clone", repo, dir, "--", "--depth", "1"); err != nil {
		return "", fmt.Errorf("clone %s: %s: %w", repo, strings.TrimSpace(string(out)), err)
	}

	changed, err := mutate(dir)
	if err != nil {
		return "", fmt.Errorf("update workflow checkout: %w", err)
	}
	if !changed {
		return "", nil
	}

	jobLabel := job.Name
	if jobLabel == "" {
		jobLabel = job.ID
	}
	title := fmt.Sprintf("chore(schedule): %s %s", verb, jobLabel)

	git := func(timeout time.Duration, args ...string) error {
		out, err := s.run(ctx, timeout, "git", append([]string{"-C", dir}, args...)...)
		if err != nil {
			return fmt.Errorf("git %s: %s: %w", args[0], strings.TrimSpace(string(out)), err)
		}
		return nil
	}

	branch := ""
	if s.cfg.PullRequests {
		branch = fmt.Sprintf("infer/schedule-%s-%d", shortID(job.ID), time.Now().Unix())
		if err := git(ghCommandTimeout, "checkout", "-b", branch); err != nil {
			return "", err
		}
	}
	if err := git(ghCommandTimeout, "add", "-A"); err != nil {
		return "", err
	}
	botName := s.cfg.BotName
	if botName == "" {
		botName = config.DefaultBotName
	}
	botEmail := s.cfg.BotEmail
	if botEmail == "" {
		botEmail = config.DefaultBotEmail
	}
	if err := git(ghCommandTimeout,
		"-c", "user.name="+botName, "-c", "user.email="+botEmail,
		"commit", "-m", title); err != nil {
		return "", err
	}

	if !s.cfg.PullRequests {
		if err := git(ghNetworkTimeout, "push", "origin", "HEAD"); err != nil {
			return "", err
		}
		return "", nil
	}

	if err := git(ghNetworkTimeout, "push", "-u", "origin", branch); err != nil {
		return "", err
	}
	out, err := s.run(ctx, ghCommandTimeout, "gh", "pr", "create",
		"--repo", repo,
		"--head", branch,
		"--title", title,
		"--body", prBody(job, verb, s.cfg))
	if err != nil {
		return "", fmt.Errorf("create PR: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ResolveRepo returns the configured repository, defaulting to
// "<authenticated user>/.routines".
func ResolveRepo(ctx context.Context, runner CommandRunner, configured string) (string, error) {
	if repo := strings.TrimSpace(configured); repo != "" {
		return repo, nil
	}
	ctx, cancel := context.WithTimeout(ctx, ghCommandTimeout)
	defer cancel()
	out, err := runner.Run(ctx, "gh", "api", "user", "-q", ".login")
	if err != nil {
		return "", fmt.Errorf("resolve GitHub user (is gh authenticated?): %s: %w", strings.TrimSpace(string(out)), err)
	}
	login := strings.TrimSpace(string(out))
	if login == "" {
		return "", errors.New("resolve GitHub user: gh returned an empty login")
	}
	return login + "/" + DefaultRepoName, nil
}

// ensureRepo creates the repository (private, with an initial commit so a
// default branch exists) when it does not exist yet.
func (s *Store) ensureRepo(ctx context.Context, repo string) error {
	if _, err := s.run(ctx, ghCommandTimeout, "gh", "repo", "view", repo, "--json", "name"); err == nil {
		return nil
	}
	out, err := s.run(ctx, ghCommandTimeout, "gh", "repo", "create", repo, "--private", "--add-readme")
	if err != nil {
		return fmt.Errorf("create repo %s: %s: %w", repo, strings.TrimSpace(string(out)), err)
	}
	return nil
}

// run executes one subprocess under its own timeout, honoring the caller ctx.
func (s *Store) run(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.runner.Run(ctx, name, args...)
}

func prBody(job *scheddomain.ScheduledJob, verb string, cfg config.SchedulerGitHubConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Scheduled job %s: %s\n\n", verb, job.ID)
	if job.Name != "" {
		fmt.Fprintf(&b, "**Name:** %s\n\n", job.Name)
	}
	fmt.Fprintf(&b, "**Cron:** `%s` (GitHub Actions schedules run in **UTC**)\n\n", job.CronExpression)
	b.WriteString("Merging this PR deploys the schedule. Nothing is pushed to the default branch directly.\n\n")
	b.WriteString("### Required repository Actions secrets\n\n")
	b.WriteString("The workflow passes these through to infer-action; set the one(s) for your provider (Settings → Secrets and variables → Actions). The CLI never writes secrets.\n\n")
	for _, s := range requiredSecrets(cfg) {
		fmt.Fprintf(&b, "- `%s`\n", s)
	}
	return b.String()
}
