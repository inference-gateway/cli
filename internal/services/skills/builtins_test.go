package skills

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestSeedBuiltins_SeedsWhenMissing(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, SeedBuiltins(dest, false))

	require.FileExists(t, filepath.Join(dest, "tmux", "SKILL.md"))

	sk, loadErr := LoadSkillMetadata(filepath.Join(dest, "tmux"), "tmux", domain.SkillScopeUser, "")
	require.Nil(t, loadErr, "seeded built-in must validate")
	require.NotNil(t, sk)
	require.Equal(t, "tmux", sk.Name)
	require.NotEmpty(t, sk.Description)
}

func TestSeedBuiltins_DoesNotOverwriteUserEdits(t *testing.T) {
	dest := t.TempDir()
	skillPath := filepath.Join(dest, "tmux", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o755))
	const sentinel = "---\nname: tmux\ndescription: My customized version.\n---\n"
	require.NoError(t, os.WriteFile(skillPath, []byte(sentinel), 0o644))

	require.NoError(t, SeedBuiltins(dest, false))

	got, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	require.Equal(t, sentinel, string(got), "seed-if-absent must not clobber a user's edit")
}

func TestSeedBuiltins_OverwriteResets(t *testing.T) {
	dest := t.TempDir()
	skillPath := filepath.Join(dest, "tmux", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o755))
	require.NoError(t, os.WriteFile(skillPath, []byte("stale\n"), 0o644))

	require.NoError(t, SeedBuiltins(dest, true))

	got, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	require.NotEqual(t, "stale\n", string(got), "overwrite must reset to the shipped default")
	require.Contains(t, string(got), "name: tmux", "reset content is the embedded skill")
}
