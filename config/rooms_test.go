package config

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// TestDefaultRoomsConfig pins the in-code default: one public room holding
// every agent via the wildcard, routing on the clef decision model, and 20
// context posts.
func TestDefaultRoomsConfig(t *testing.T) {
	cfg := DefaultRoomsConfig()

	require.Equal(t, DefaultRoomsMinConfidence, cfg.Router.MinConfidence)
	require.Equal(t, RoomsFallbackTop, cfg.Router.Fallback)
	require.Equal(t, "ollama_cloud/clef:27b", cfg.Router.Model, "a fresh room routes unpinged posts on clef")
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
	redefined := RoomConfig{Name: "public", Project: "/repo/app", Agents: []string{"tester"}}
	release := RoomConfig{Name: "release", Agents: []string{"writer"}}

	tests := []struct {
		name      string
		config    RoomsConfig
		wantRooms []RoomConfig
	}{
		{
			name:      "a file without a rooms list keeps the default",
			config:    RoomsConfig{Router: RoomsRouterConfig{Model: "decision/falcon"}},
			wantRooms: DefaultRoomsConfig().Rooms,
		},
		{
			name:      "a rooms list without public keeps the default too",
			config:    RoomsConfig{Rooms: []RoomConfig{release}},
			wantRooms: []RoomConfig{release, DefaultRoomsConfig().Rooms[0]},
		},
		{
			name:      "a public room the file defines replaces the default",
			config:    RoomsConfig{Rooms: []RoomConfig{redefined}},
			wantRooms: []RoomConfig{redefined},
		},
		{
			name:      "an empty rooms list keeps the default",
			config:    RoomsConfig{Rooms: []RoomConfig{}},
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
		{"a provider/model router model is valid", RoomsConfig{Router: RoomsRouterConfig{Model: "anthropic/claude-haiku-5-5"}}, ""},
		{"a router model without a provider fails", RoomsConfig{Router: RoomsRouterConfig{Model: "claude-haiku-5-5"}}, "rooms.router.model"},
		{"a negative max_members fails", RoomsConfig{Router: RoomsRouterConfig{MaxMembers: -1}}, "rooms.router.max_members"},
		{"a name that escapes the rooms dir fails", RoomsConfig{Rooms: []RoomConfig{{Name: "../evil"}}}, "name"},
		{"a duplicate name fails", RoomsConfig{Rooms: []RoomConfig{{Name: "public"}, {Name: "public"}}}, "defined twice"},
		{"a human mode for members is valid", RoomsConfig{Rooms: []RoomConfig{{Name: "release", Mode: "standard"}}}, ""},
		{"an unknown member mode fails", RoomsConfig{Rooms: []RoomConfig{{Name: "release", Mode: "yolo"}}}, "mode"},
		{"the subagent-only readonly mode fails", RoomsConfig{Rooms: []RoomConfig{{Name: "release", Mode: "readonly"}}}, "mode"},
		{"a top-match pickup is valid", RoomsConfig{Rooms: []RoomConfig{{Name: "release", Pickup: roomsdomain.PickupTop}}}, ""},
		{"an unknown pickup fails", RoomsConfig{Rooms: []RoomConfig{{Name: "release", Pickup: "everyone"}}}, "pickup"},
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

func TestNeedsGatewayDecisions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   bool
	}{
		{"the default rooms router needs them", func(*Config) {}, true},
		{"an empty router model and the llm judge need none", func(c *Config) { c.Rooms.Router.Model = "" }, false},
		{"the judge's decisions backend needs them", func(c *Config) {
			c.Rooms.Router.Model = ""
			c.Judge.Backend = JudgeBackendDecisions
		}, true},
		{"a rooms router model needs them", func(c *Config) { c.Rooms.Router.Model = "anthropic/claude-haiku-5-5" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(cfg)
			require.Equal(t, tt.want, cfg.NeedsGatewayDecisions())
		})
	}
}
