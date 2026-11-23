package config

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"
)

// TestDefaultRoomsConfig pins the in-code default: one public room holding
// every agent via the wildcard, @mentions-only routing, and 20 context posts.
func TestDefaultRoomsConfig(t *testing.T) {
	cfg := DefaultRoomsConfig()

	require.Equal(t, DefaultRoomsMinConfidence, cfg.Router.MinConfidence)
	require.Equal(t, RoomsFallbackTop, cfg.Router.Fallback)
	require.Equal(t, "", cfg.Router.Model, "an empty router model routes @mentions only")
	require.Equal(t, DefaultRoomsContextPosts, cfg.ContextPosts)

	require.Len(t, cfg.Rooms, 1)
	require.Equal(t, "public", cfg.Rooms[0].Name)
	require.Equal(t, []string{"*"}, cfg.Rooms[0].Agents, "the public room holds every agent")
	require.Equal(t, "", cfg.Rooms[0].Project, "the public room runs in the default project dir")
}

// TestRoomsConfigWithDefaultRoom covers the replacement rule: a rooms.yaml that
// defines public itself replaces the default room, and one without it keeps the
// default.
func TestRoomsConfigWithDefaultRoom(t *testing.T) {
	redefined := RoomsRoomConfig{Name: "public", Project: "/repo/app", Agents: []string{"tester"}}
	release := RoomsRoomConfig{Name: "release", Agents: []string{"writer"}}

	tests := []struct {
		name      string
		config    RoomsConfig
		wantRooms []RoomsRoomConfig
	}{
		{
			name:      "a file without a rooms list keeps the default",
			config:    RoomsConfig{Router: RoomsRouterConfig{Model: "decision/falcon"}},
			wantRooms: DefaultRoomsConfig().Rooms,
		},
		{
			name:      "a rooms list without public keeps the default too",
			config:    RoomsConfig{Rooms: []RoomsRoomConfig{release}},
			wantRooms: []RoomsRoomConfig{release, DefaultRoomsConfig().Rooms[0]},
		},
		{
			name:      "a public room the file defines replaces the default",
			config:    RoomsConfig{Rooms: []RoomsRoomConfig{redefined}},
			wantRooms: []RoomsRoomConfig{redefined},
		},
		{
			name:      "an empty rooms list keeps the default",
			config:    RoomsConfig{Rooms: []RoomsRoomConfig{}},
			wantRooms: DefaultRoomsConfig().Rooms,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := &tt.config
			got := tt.config.WithDefaultRoom()
			require.Equal(t, tt.wantRooms, got.Rooms)
			require.Equal(t, before.Router, got.Router, "the router is left alone")
			require.Equal(t, before.ContextPosts, got.ContextPosts)
		})
	}
}

func TestRoomsConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		config RoomsConfig
		errOn  string
	}{
		{"defaults are valid", *DefaultRoomsConfig(), ""},
		{"top fallback is valid", RoomsConfig{Router: RoomsRouterConfig{Fallback: RoomsFallbackTop}}, ""},
		{"unknown fallback fails", RoomsConfig{Router: RoomsRouterConfig{Fallback: "first"}}, "rooms.router.fallback"},
		{"confidence above one fails", RoomsConfig{Router: RoomsRouterConfig{MinConfidence: 1.1}}, "min_confidence"},
		{"negative context posts fails", RoomsConfig{ContextPosts: -1}, "context_posts"},
		{"a name that escapes the rooms dir fails", RoomsConfig{Rooms: []RoomsRoomConfig{{Name: "../evil"}}}, "name"},
		{"a duplicate name fails", RoomsConfig{Rooms: []RoomsRoomConfig{{Name: "public"}, {Name: "public"}}}, "defined twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.errOn == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.errOn)
		})
	}
}

// TestSaveRoomsBlockStyle pins that the seeded rooms.yaml writes the agent
// list and room entries as YAML block style, never flow style.
func TestSaveRoomsBlockStyle(t *testing.T) {
	path := filepath.Join(t.TempDir(), RoomsFileName)
	require.NoError(t, SaveRooms(path, DefaultRoomsConfig()))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	require.Contains(t, content, "- name: public", "the room entries are block style")
	require.Contains(t, content, "- '*'", "the agent list is block style")
	require.NotContains(t, content, "['*']", "no flow-style sequences")
	require.NotContains(t, content, "{name:", "no flow-style mappings")
}
