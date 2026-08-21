package plugins

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

// TestParseSourceLocalPath covers the local-directory branch; the GitHub
// shorthand and URL forms are covered by TestParseSource in installer_test.go.
func TestParseSourceLocalPath(t *testing.T) {
	dir := t.TempDir()

	t.Run("absolute directory resolves to itself", func(t *testing.T) {
		got, err := ParseSource(dir)
		require.NoError(t, err)
		require.Equal(t, Source{Kind: SourceLocal, Path: dir, Raw: dir}, got)
	})

	t.Run("relative directory resolves to an absolute path", func(t *testing.T) {
		nested := filepath.Join(dir, "plugin")
		require.NoError(t, os.Mkdir(nested, 0o755))
		t.Chdir(dir)

		got, err := ParseSource("./plugin")
		require.NoError(t, err)
		require.True(t, filepath.IsAbs(got.Path), "want absolute path, got %q", got.Path)
		require.Equal(t, SourceLocal, got.Kind)
		require.Equal(t, "plugin", filepath.Base(got.Path))
	})

	t.Run("a file is not a plugin directory", func(t *testing.T) {
		file := filepath.Join(dir, "plugin.json")
		require.NoError(t, os.WriteFile(file, []byte("{}"), 0o600))
		_, err := ParseSource(file)
		require.ErrorContains(t, err, "is not a directory")
	})

	t.Run("missing path errors", func(t *testing.T) {
		_, err := ParseSource(filepath.Join(dir, "does-not-exist"))
		require.ErrorContains(t, err, "local plugin path")
	})
}
