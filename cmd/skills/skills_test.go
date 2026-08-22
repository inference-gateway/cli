package skills

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

func TestShortenPath(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	require.Empty(t, shortenPath(""), "a not-installed skill has no path")

	require.Equal(t, filepath.Join(".agents", "skills", "go", "SKILL.md"),
		shortenPath(filepath.Join(cwd, ".agents", "skills", "go", "SKILL.md")),
		"a project skill renders relative to the working directory")

	require.Equal(t, filepath.Join("~", ".infer", "skills", "tmux", "SKILL.md"),
		shortenPath(filepath.Join(home, ".infer", "skills", "tmux", "SKILL.md")),
		"a user-global skill renders ~-prefixed, not as ../.. from cwd")

	require.Equal(t, filepath.Join("/opt", "skills", "x", "SKILL.md"),
		shortenPath(filepath.Join("/opt", "skills", "x", "SKILL.md")),
		"a path under neither stays absolute")
}
