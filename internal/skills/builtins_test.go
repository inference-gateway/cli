package skills

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestSeedBuiltins_SeedsWhenMissing(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, SeedBuiltins(dest, false))

	for _, name := range []string{"tmux", "bug"} {
		sk, loadErr := LoadSkillMetadata(filepath.Join(dest, name), name, agentdomain.SkillScopeUser, "")
		require.Nil(t, loadErr, "seeded built-in must validate")
		require.NotNil(t, sk)
		require.Equal(t, name, sk.Name)
		require.NotEmpty(t, sk.Description)
	}

	bug, loadErr := LoadSkillMetadata(filepath.Join(dest, "bug"), "bug", agentdomain.SkillScopeUser, "")
	require.Nil(t, loadErr)
	require.Contains(t, bug.Description, "/bug", "description must route /bug invocations")
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
