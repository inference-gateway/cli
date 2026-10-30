package runtime

import (
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

func TestConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	tests := []struct {
		name      string
		toProject bool
		want      string
	}{
		{"userspace baseline by default", false, filepath.Join(home, config.ConfigDirName, config.MCPFileName)},
		{"project override with --project", true, filepath.Join(config.ConfigDirName, config.MCPFileName)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConfigPath(tt.toProject, config.MCPFileName)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUserConfigPathFailsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	_, err := UserConfigPath(config.PluginsFileName)
	require.Error(t, err)
}
