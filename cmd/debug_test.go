package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// The fresh-CI-runner case: the git memory backend is configured but nothing
// has cloned the memory repo yet. renderAgentSystemPrompt must SyncIn before
// building the prompt so the PERSISTENT MEMORY INDEX section renders.
func TestRenderAgentSystemPrompt_SyncsGitMemoryIn(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "no-such-gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "infer-test")
	t.Setenv("GIT_AUTHOR_EMAIL", "infer-test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "infer-test")
	t.Setenv("GIT_COMMITTER_EMAIL", "infer-test@example.com")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")

	mustGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if dir != "" {
			cmd.Dir = dir
		}
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	bare := filepath.Join(t.TempDir(), "remote.git")
	mustGit("", "init", "--bare", "-b", "main", bare)
	work := t.TempDir()
	mustGit("", "clone", bare, work)
	mustGit(work, "checkout", "-B", "main")
	require.NoError(t, os.WriteFile(filepath.Join(work, "MEMORY.md"),
		[]byte("# Memory Index\n\n- [ci-fact](ci-fact.md) - proves debug sync-in ran\n"), 0o600))
	mustGit(work, "add", "-A")
	mustGit(work, "commit", "-m", "seed memory")
	mustGit(work, "push", "-u", "origin", "main")

	memDir := filepath.Join(t.TempDir(), "memory")

	cfg := config.DefaultConfig()
	cfg.Gateway.Run = false
	cfg.Storage.Enabled = false
	cfg.Prompts.Agent.SystemPrompt = "You are a test agent."
	cfg.Memory.Enabled = true
	cfg.Memory.Dir = memDir
	cfg.Memory.Backend.Type = config.MemoryBackendGit
	cfg.Memory.Backend.Git.Repo = bare
	cfg.Memory.Backend.Git.Branch = "main"

	got := renderAgentSystemPrompt(context.Background(), cfg)

	require.Contains(t, got, "PERSISTENT MEMORY INDEX")
	require.Contains(t, got, "ci-fact")
	require.Contains(t, got, volatileTailDivider)
	require.Less(t, strings.Index(got, volatileTailDivider), strings.Index(got, "PERSISTENT MEMORY INDEX"),
		"memory index renders in the volatile tail, below the divider")
}
