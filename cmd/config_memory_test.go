package cmd

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestApplyMemoryEnvOverrides_Backend(t *testing.T) {
	t.Setenv("INFER_MEMORY_BACKEND_TYPE", "git")
	t.Setenv("INFER_MEMORY_BACKEND_GIT_REPO", "git@github.com:o/r.git")
	t.Setenv("INFER_MEMORY_BACKEND_GIT_BRANCH", "prod")
	t.Setenv("INFER_MEMORY_BACKEND_GIT_COMMIT_MESSAGE", "sync memory")
	t.Setenv("INFER_MEMORY_BACKEND_GIT_TIMEOUT", "30")
	t.Setenv("INFER_MEMORY_BACKEND_GIT_SYNC_ON_START", "off")
	t.Setenv("INFER_MEMORY_BACKEND_GIT_SYNC_ON_FINISH", "push")

	cfg := &config.Config{}
	applyMemoryEnvOverrides(cfg)

	g := cfg.Memory.Backend
	if g.Type != "git" {
		t.Errorf("Type = %q, want git", g.Type)
	}
	if g.Git.Repo != "git@github.com:o/r.git" {
		t.Errorf("Repo = %q", g.Git.Repo)
	}
	if g.Git.Branch != "prod" {
		t.Errorf("Branch = %q, want prod", g.Git.Branch)
	}
	if g.Git.CommitMessage != "sync memory" {
		t.Errorf("CommitMessage = %q", g.Git.CommitMessage)
	}
	if g.Git.Timeout != 30 {
		t.Errorf("Timeout = %d, want 30", g.Git.Timeout)
	}
	if g.Git.Sync.OnStart != "off" || g.Git.Sync.OnFinish != "push" {
		t.Errorf("Sync = %+v", g.Git.Sync)
	}
}

func TestPruneMemoryReminderIfDisabled(t *testing.T) {
	memoryReminders := func(rs []config.ReminderConfig) int {
		n := 0
		for _, r := range rs {
			if r.Name == "memory-consult" || r.Name == "memory-hygiene" {
				n++
			}
		}
		return n
	}

	disabled := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	disabled.Memory.Enabled = false
	pruneMemoryReminderIfDisabled(disabled)
	if got := memoryReminders(disabled.Reminders.Reminders); got != 0 {
		t.Errorf("memory reminders should be pruned when memory disabled; got %d", got)
	}
	hasTodo := false
	for _, r := range disabled.Reminders.Reminders {
		if r.Name == "todo-hygiene" {
			hasTodo = true
		}
	}
	if !hasTodo {
		t.Error("todo-hygiene should remain after pruning memory reminders")
	}

	enabled := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	enabled.Memory.Enabled = true
	pruneMemoryReminderIfDisabled(enabled)
	if got := memoryReminders(enabled.Reminders.Reminders); got != 2 {
		t.Errorf("both memory reminders should remain when memory enabled; got %d", got)
	}
}
