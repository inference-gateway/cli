package plugins

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

// writeManifest seeds <dir>/.claude-plugin/plugin.json with body.
func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(config.PluginManifestPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func TestParseManifest(t *testing.T) {
	t.Run("absent manifest yields no manifest and no error", func(t *testing.T) {
		m, err := parseManifest(t.TempDir())
		require.NoError(t, err)
		require.Nil(t, m)
	})

	t.Run("malformed json errors", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, "{not json")
		_, err := parseManifest(dir)
		require.ErrorContains(t, err, config.PluginManifestPath)
	})

	t.Run("parses name, version and capability presence", func(t *testing.T) {
		dir := t.TempDir()
		writeManifest(t, dir, `{
			"name": "ponytail",
			"version": "1.2.3",
			"description": "be lazy",
			"author": {"name": "Eden", "url": "https://example.com"},
			"hooks": {"PreToolUse": []}
		}`)

		m, err := parseManifest(dir)
		require.NoError(t, err)
		require.NotNil(t, m)
		require.Equal(t, "ponytail", m.Name)
		require.Equal(t, "1.2.3", m.Version)
		require.Equal(t, "be lazy", m.Description)
		require.Equal(t, "Eden", m.Author.Name)
		require.NotNil(t, m.Hooks, "hooks key present should be detectable")
		require.Nil(t, m.Commands, "absent commands key stays nil")
		require.Nil(t, m.MCP, "absent mcpServers key stays nil")
	})
}
