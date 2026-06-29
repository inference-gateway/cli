package config

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

// chdirTemp switches into a fresh temp dir and restores the cwd on cleanup.
// These tests must not run in parallel: they mutate the process working dir.
func chdirTemp(t *testing.T) string {
	t.Helper()
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	return dir
}

func TestEnsureProjectGitignoreCreatesInFreshDir(t *testing.T) {
	chdirTemp(t)

	require.NoError(t, EnsureProjectGitignore())

	data, err := os.ReadFile(filepath.Join(ConfigDirName, GitignoreFileName))
	require.NoError(t, err)
	require.Equal(t, InferGitignoreContent, string(data))
	require.Contains(t, string(data), "backups/")
}

// EnsureProjectGitignore must never clobber an existing file so that
// `infer init --project` output and any user edits survive.
func TestEnsureProjectGitignoreDoesNotOverwriteExisting(t *testing.T) {
	chdirTemp(t)

	path := filepath.Join(ConfigDirName, GitignoreFileName)
	require.NoError(t, os.MkdirAll(ConfigDirName, 0o755))
	const sentinel = "# custom\nmystuff/\n"
	require.NoError(t, os.WriteFile(path, []byte(sentinel), 0o644))

	require.NoError(t, EnsureProjectGitignore())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, sentinel, string(data))
}
