package githubscheduler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

// scriptRunner records every command and answers via respond; nil respond
// means every command succeeds with empty output.
type scriptRunner struct {
	commands []string
	respond  func(name string, args []string) ([]byte, error)
}

func (r *scriptRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	if r.respond != nil {
		return r.respond(name, args)
	}
	return nil, nil
}

// has reports whether any recorded command contains all the given substrings.
func (r *scriptRunner) has(subs ...string) bool {
	for _, cmd := range r.commands {
		ok := true
		for _, s := range subs {
			if !strings.Contains(cmd, s) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func newTestStore(runner *scriptRunner, cfg config.SchedulerGitHubConfig) (*Store, storage.ScheduledJobStorage) {
	inner := storage.NewMemoryStorage()
	return NewStore(inner, runner, cfg, "anthropic/claude"), inner
}

func TestSaveJobDirectPush(t *testing.T) {
	runner := &scriptRunner{}
	store, inner := newTestStore(runner, config.SchedulerGitHubConfig{Repository: "me/.routines"})

	job := testJob()
	if err := store.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}

	for _, want := range [][]string{
		{"gh repo view me/.routines"},
		{"gh repo clone me/.routines"},
		{"git", "add -A"},
		{"git", "commit -m chore(schedule): upsert morning briefing"},
		{"git", "push origin HEAD"},
	} {
		if !runner.has(want...) {
			t.Errorf("missing command %v in %v", want, runner.commands)
		}
	}
	if runner.has("gh pr create") {
		t.Errorf("direct-push mode must not open a PR: %v", runner.commands)
	}
	if runner.has("git checkout -b") {
		t.Errorf("direct-push mode must not branch: %v", runner.commands)
	}
	if store.LastPRURL() != "" {
		t.Errorf("LastPRURL = %q, want empty", store.LastPRURL())
	}
	if _, err := inner.LoadJob(context.Background(), job.ID); err != nil {
		t.Errorf("job not persisted locally: %v", err)
	}
}

func TestSaveJobPullRequestMode(t *testing.T) {
	runner := &scriptRunner{respond: func(name string, args []string) ([]byte, error) {
		if name == "gh" && args[0] == "pr" {
			return []byte("https://github.com/me/.routines/pull/7\n"), nil
		}
		return nil, nil
	}}
	store, _ := newTestStore(runner, config.SchedulerGitHubConfig{Repository: "me/.routines", PullRequests: true})

	if err := store.SaveJob(context.Background(), testJob()); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}
	if !runner.has("git", "checkout -b infer/schedule-0b7f4a2c") {
		t.Errorf("missing branch creation: %v", runner.commands)
	}
	if !runner.has("gh pr create --repo me/.routines") {
		t.Errorf("missing pr create: %v", runner.commands)
	}
	if got := store.LastPRURL(); got != "https://github.com/me/.routines/pull/7" {
		t.Errorf("LastPRURL = %q", got)
	}
}

func TestSaveJobCreatesMissingRepo(t *testing.T) {
	runner := &scriptRunner{respond: func(name string, args []string) ([]byte, error) {
		if name == "gh" && args[0] == "repo" && args[1] == "view" {
			return []byte("Could not resolve to a Repository"), errors.New("exit 1")
		}
		return nil, nil
	}}
	store, _ := newTestStore(runner, config.SchedulerGitHubConfig{Repository: "me/.routines"})

	if err := store.SaveJob(context.Background(), testJob()); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}
	if !runner.has("gh repo create me/.routines --private --add-readme") {
		t.Errorf("missing repo create: %v", runner.commands)
	}
}

func TestSaveJobResolvesDefaultRepo(t *testing.T) {
	runner := &scriptRunner{respond: func(name string, args []string) ([]byte, error) {
		if name == "gh" && args[0] == "api" && args[1] == "user" {
			return []byte("someone\n"), nil
		}
		return nil, nil
	}}
	store, _ := newTestStore(runner, config.SchedulerGitHubConfig{})

	if err := store.SaveJob(context.Background(), testJob()); err != nil {
		t.Fatalf("SaveJob: %v", err)
	}
	if !runner.has("gh repo view someone/.routines") {
		t.Errorf("default repo not resolved: %v", runner.commands)
	}
}

func TestSaveJobBadCronRejectedBeforeAnyCommand(t *testing.T) {
	runner := &scriptRunner{}
	store, inner := newTestStore(runner, config.SchedulerGitHubConfig{Repository: "me/.routines"})

	job := testJob()
	job.CronExpression = "@every 7m"
	err := store.SaveJob(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "GitHub Actions cron") {
		t.Fatalf("want cron rejection, got %v", err)
	}
	if len(runner.commands) != 0 {
		t.Errorf("no gh/git command should run on invalid cron: %v", runner.commands)
	}
	if _, err := inner.LoadJob(context.Background(), job.ID); !errors.Is(err, storage.ErrJobNotFound) {
		t.Errorf("job must not be persisted locally: %v", err)
	}
}

func TestSaveJobGhFailureLeavesLocalUnchanged(t *testing.T) {
	runner := &scriptRunner{respond: func(name string, args []string) ([]byte, error) {
		if name == "git" && slices.Contains(args, "push") {
			return []byte("permission denied"), fmt.Errorf("exit 1")
		}
		return nil, nil
	}}
	store, inner := newTestStore(runner, config.SchedulerGitHubConfig{Repository: "me/.routines"})

	job := testJob()
	err := store.SaveJob(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("want push failure, got %v", err)
	}
	if _, err := inner.LoadJob(context.Background(), job.ID); !errors.Is(err, storage.ErrJobNotFound) {
		t.Errorf("failed sync must not persist the job locally: %v", err)
	}
}

func TestDeleteJobRemovesWorkflow(t *testing.T) {
	runner := &scriptRunner{}
	store, inner := newTestStore(runner, config.SchedulerGitHubConfig{Repository: "me/.routines"})
	job := testJob()
	if err := inner.SaveJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteJob(context.Background(), job.ID); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	if runner.has("git", "push") {
		t.Errorf("nothing to remove -> no push expected: %v", runner.commands)
	}
	if _, err := inner.LoadJob(context.Background(), job.ID); !errors.Is(err, storage.ErrJobNotFound) {
		t.Errorf("job should be deleted locally: %v", err)
	}
}

func TestSaveJobRunOnceAllowed(t *testing.T) {
	runner := &scriptRunner{}
	store, _ := newTestStore(runner, config.SchedulerGitHubConfig{Repository: "me/.routines"})
	job := testJob()
	job.RunOnce = true
	if err := store.SaveJob(context.Background(), job); err != nil {
		t.Fatalf("SaveJob run_once: %v", err)
	}
}

func TestWorkflowFilePath(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"plain", "abc", "/checkout/.github/workflows/abc.yml"},
		{"uuid", "6f1c0f6e-2f7a-4d3e-9a1b-0c2d3e4f5a6b", "/checkout/.github/workflows/6f1c0f6e-2f7a-4d3e-9a1b-0c2d3e4f5a6b.yml"},
		{"parent traversal", "../evil", "/checkout/.github/evil.yml"},
		{"deep traversal", "a/../../b", "/checkout/.github/b.yml"},
		{"absolute", "/etc/passwd", "/checkout/.github/workflows/etc/passwd.yml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := workflowFilePath("/checkout", tt.id)
			if got != tt.want {
				t.Errorf("workflowFilePath(%q) = %q, want %q", tt.id, got, tt.want)
			}
			if !strings.HasPrefix(got, "/checkout/") {
				t.Errorf("workflowFilePath(%q) = %q escapes the checkout dir", tt.id, got)
			}
		})
	}
}

func TestSaveJobInvalidIDRejectedBeforeAnyCommand(t *testing.T) {
	runner := &scriptRunner{}
	store, inner := newTestStore(runner, config.SchedulerGitHubConfig{Repository: "me/.routines"})

	job := testJob()
	job.ID = "../escape"
	err := store.SaveJob(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "invalid job ID") {
		t.Fatalf("want invalid job ID rejection, got %v", err)
	}
	if len(runner.commands) != 0 {
		t.Errorf("no gh/git command should run on invalid job ID: %v", runner.commands)
	}
	if _, err := inner.LoadJob(context.Background(), job.ID); err == nil {
		t.Errorf("job must not be persisted locally")
	}
}
