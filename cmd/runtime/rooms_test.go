package runtime

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

func writeRoomsYAML(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func roomNamesOf(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Rooms.Rooms))
	for _, room := range cfg.Rooms.Rooms {
		names = append(names, room.Name)
	}
	return names
}

// TestRoomsSidecarLoading covers rooms.yaml as a split sidecar under the rooms
// key, in the home and project layers: which file defines what and when the
// default public room survives.
func TestRoomsSidecarLoading(t *testing.T) {
	const releaseOnly = "---\nrooms:\n  - name: release\n    agents:\n      - writer\n"
	const publicRedefined = "---\nrooms:\n  - name: public\n    project: /Users/me/Repositories/app\n    agents:\n      - researcher\n"

	tests := []struct {
		name        string
		homeYAML    string
		projectYAML string
		check       func(t *testing.T, cfg *config.Config)
	}{
		{
			name: "no file keeps the defaults",
			check: func(t *testing.T, cfg *config.Config) {
				require.Equal(t, config.DefaultRoomsConfig(), &cfg.Rooms)
			},
		},
		{
			name:     "a rooms.yaml without public keeps the default room",
			homeYAML: releaseOnly,
			check: func(t *testing.T, cfg *config.Config) {
				require.Equal(t, []string{"release", "public"}, roomNamesOf(cfg))
				require.Equal(t, config.DefaultRoomsConfig().Rooms[0], cfg.Rooms.Rooms[1], "the kept default is the default room")
			},
		},
		{
			name:     "a rooms.yaml that defines public itself replaces the default",
			homeYAML: publicRedefined,
			check: func(t *testing.T, cfg *config.Config) {
				require.Equal(t, []string{"public"}, roomNamesOf(cfg))
				room := cfg.Rooms.Rooms[0]
				require.Equal(t, "/Users/me/Repositories/app", room.Project)
				require.Equal(t, []string{"researcher"}, room.Agents, "the file's agents win over the wildcard")
			},
		},
		{
			name:     "router knobs load from the file and keep unmatched defaults",
			homeYAML: "---\nrouter:\n  min_confidence: 0.9\nrooms:\n  - name: release\n    agents:\n      - writer\n",
			check: func(t *testing.T, cfg *config.Config) {
				require.InDelta(t, 0.9, cfg.Rooms.Router.MinConfidence, 1e-9)
				require.Equal(t, config.RoomsFallbackTop, cfg.Rooms.Router.Fallback, "a knob the file omits keeps its default")
				require.Equal(t, config.DefaultRoomsContextPosts, cfg.Rooms.ContextPosts)
			},
		},
		{
			name:        "the project layer wins over the home layer",
			homeYAML:    "---\nrooms:\n  - name: public\n    agents:\n      - home_agent\n",
			projectYAML: "---\nrooms:\n  - name: public\n    agents:\n      - project_agent\n",
			check: func(t *testing.T, cfg *config.Config) {
				require.Equal(t, []string{"public"}, roomNamesOf(cfg))
				require.Equal(t, []string{"project_agent"}, cfg.Rooms.Rooms[0].Agents)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir, projectDir := splitHomeProjectEnv(t)
			if tt.homeYAML != "" {
				writeRoomsYAML(t, filepath.Join(homeDir, config.ConfigDirName, config.RoomsFileName), tt.homeYAML)
			}
			if tt.projectYAML != "" {
				writeRoomsYAML(t, filepath.Join(projectDir, config.ConfigDirName, config.RoomsFileName), tt.projectYAML)
			}
			tt.check(t, loadTestConfig(t))
		})
	}
}
