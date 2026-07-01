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

func TestReconcileMemoryReminders(t *testing.T) {
	countMemory := func(rs []config.ReminderConfig) int {
		n := 0
		for _, r := range rs {
			if r.Name == "memory-consult" || r.Name == "memory-hygiene" {
				n++
			}
		}
		return n
	}
	has := func(rs []config.ReminderConfig, name string) bool {
		for _, r := range rs {
			if r.Name == name {
				return true
			}
		}
		return false
	}

	disabled := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	disabled.Memory.Enabled = false
	reconcileMemoryReminders(disabled)
	if got := countMemory(disabled.Reminders.Reminders); got != 0 {
		t.Errorf("memory reminders should be pruned when memory disabled; got %d", got)
	}
	if !has(disabled.Reminders.Reminders, "todo-hygiene") {
		t.Error("todo-hygiene should remain after pruning memory reminders")
	}

	legacy := &config.Config{Reminders: config.RemindersConfig{
		Enabled: true,
		Reminders: []config.ReminderConfig{
			{Name: "todo-hygiene", Hook: "pre_stream", Trigger: config.ReminderTriggerInterval, Interval: 4, Text: "x"},
		},
	}}
	legacy.Memory.Enabled = true
	reconcileMemoryReminders(legacy)
	if !has(legacy.Reminders.Reminders, "memory-hygiene") || !has(legacy.Reminders.Reminders, "memory-consult") {
		t.Errorf("memory reminders should be injected into a legacy config when memory enabled: %+v", legacy.Reminders.Reminders)
	}

	full := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	full.Memory.Enabled = true
	reconcileMemoryReminders(full)
	if got := countMemory(full.Reminders.Reminders); got != 2 {
		t.Errorf("expected exactly 2 memory reminders (no duplicates); got %d", got)
	}
}
