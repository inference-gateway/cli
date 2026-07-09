package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatProjectListing(t *testing.T) {
	t.Run("root files first then shallow directories", func(t *testing.T) {
		got := formatProjectListing([]string{
			"internal/agent/agent.go",
			"cmd/root.go",
			"main.go",
			"go.mod",
		}, 100, 25)

		lines := strings.Split(got, "\n")
		require.Equal(t, []string{
			"./: go.mod,main.go",
			"cmd/: root.go",
			"internal/agent/: agent.go",
		}, lines)
	})

	t.Run("caps lines and files per directory with markers", func(t *testing.T) {
		paths := []string{"a/one.go", "a/two.go", "a/three.go", "b/x.go", "c/y.go"}
		got := formatProjectListing(paths, 2, 2)

		require.Contains(t, got, "a/: one.go,three.go,+1 more")
		require.Contains(t, got, "... +1 more directories")
		require.NotContains(t, got, "c/:")
	})

	t.Run("omits hidden files and directories", func(t *testing.T) {
		got := formatProjectListing([]string{
			".gitignore",
			".agents/skills/go/SKILL.md",
			"internal/.hidden/secret.go",
			"main.go",
		}, 100, 25)

		require.Equal(t, "./: main.go", got)
	})

	t.Run("collapses test siblings into star marker", func(t *testing.T) {
		got := formatProjectListing([]string{
			"pkg/agent.go",
			"pkg/agent_test.go",
			"pkg/orphan_test.go",
			"pkg/util.go",
		}, 100, 25)

		require.Equal(t, "pkg/: agent.go*,orphan_test.go,util.go", got)
	})

	t.Run("empty input yields empty output", func(t *testing.T) {
		require.Empty(t, formatProjectListing(nil, 100, 25))
	})
}
