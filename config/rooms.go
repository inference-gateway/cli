package config

import (
	"fmt"
	"slices"

	configutils "github.com/inference-gateway/cli/config/utils"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

const RoomsFileName = "rooms.yaml"

// Router fallbacks for RoomsRouterConfig.Fallback: what happens to a post the
// router could not confidently address. Top hands it to the room's top match,
// none addresses nobody.
const (
	RoomsFallbackTop  = "top"
	RoomsFallbackNone = "none"
)

const (
	DefaultRoomName           = "public"
	DefaultRoomsContextPosts  = 20
	DefaultRoomsMinConfidence = 0.5
	DefaultRoomsMaxMembers    = 2
)

// RoomsConfig is the content of rooms.yaml: the router that decides who a post
// goes to, how many past posts ride along as context, and the rooms themselves.
type RoomsConfig struct {
	Router       RoomsRouterConfig `yaml:"router" mapstructure:"router"`
	ContextPosts int               `yaml:"context_posts" mapstructure:"context_posts"`
	Rooms        []RoomConfig      `yaml:"rooms" mapstructure:"rooms"`
}

// RoomsRouterConfig decides who a post is addressed to. An empty model routes
// @mentions only, and a set model is the "provider/model" decision model asked
// with the room's members. MaxMembers caps how many of them answer a post that
// mentions nobody, the most relevant first, so a busy room stays quiet. 0 means
// DefaultRoomsMaxMembers.
type RoomsRouterConfig struct {
	Model         string  `yaml:"model" mapstructure:"model"`
	MinConfidence float64 `yaml:"min_confidence" mapstructure:"min_confidence"`
	MaxMembers    int     `yaml:"max_members" mapstructure:"max_members"`
	Fallback      string  `yaml:"fallback" mapstructure:"fallback"`
}

// RoomConfig is one room. "*" as an agent reserves a seat for every agent
// that exists when the room is read, built-ins included. An empty project runs
// the members in the daemon's default project dir.
type RoomConfig struct {
	Name    string   `yaml:"name" mapstructure:"name"`
	Project string   `yaml:"project" mapstructure:"project"`
	Agents  []string `yaml:"agents" mapstructure:"agents"`
}

// DefaultRoomsConfig returns the in-code default rooms configuration used when
// no rooms.yaml exists and the file `infer init` seeds: the public room with
// every agent, @mentions-only routing and 20 context posts.
func DefaultRoomsConfig() *RoomsConfig {
	return &RoomsConfig{
		Router: RoomsRouterConfig{
			Model:         "",
			MinConfidence: DefaultRoomsMinConfidence,
			MaxMembers:    DefaultRoomsMaxMembers,
			Fallback:      RoomsFallbackTop,
		},
		ContextPosts: DefaultRoomsContextPosts,
		Rooms: []RoomConfig{
			{Name: DefaultRoomName, Agents: []string{roomsdomain.AgentsWildcard}},
		},
	}
}

// WithDefaultRoom returns the rooms with the default public room appended when
// none of them is named public. A rooms.yaml that defines public itself
// replaces the default, and one without any rooms list keeps the defaults
// Viper registered.
func (c RoomsConfig) WithDefaultRoom() RoomsConfig {
	if slices.ContainsFunc(c.Rooms, func(room RoomConfig) bool { return room.Name == DefaultRoomName }) {
		return c
	}
	c.Rooms = append(slices.Clone(c.Rooms), DefaultRoomsConfig().Rooms...)
	return c
}

// SaveRooms writes the rooms configuration to disk, creating any missing
// parent directories.
func SaveRooms(path string, cfg *RoomsConfig) error {
	return configutils.SaveYAML(path, "rooms", cfg)
}

// Validate checks the rooms config: the fallback is a known one, a router
// model is provider/model, the confidence bar is a probability, the member cap
// is not negative, and every room has a unique, filesystem-safe name.
func (c RoomsConfig) Validate() error {
	switch c.Router.Fallback {
	case "", RoomsFallbackTop, RoomsFallbackNone:
	default:
		return fmt.Errorf("rooms.router.fallback %q: must be %q or %q", c.Router.Fallback, RoomsFallbackTop, RoomsFallbackNone)
	}
	if provider, _ := ParseModel(c.Router.Model); c.Router.Model != "" && provider == "" {
		return fmt.Errorf("rooms.router.model %q: must be provider/model", c.Router.Model)
	}
	if c.Router.MinConfidence < 0 || c.Router.MinConfidence > 1 {
		return fmt.Errorf("rooms.router.min_confidence %v: must be between 0 and 1", c.Router.MinConfidence)
	}
	if c.Router.MaxMembers < 0 {
		return fmt.Errorf("rooms.router.max_members %d: must be >= 0", c.Router.MaxMembers)
	}
	if c.ContextPosts < 0 {
		return fmt.Errorf("rooms.context_posts %d: must be >= 0", c.ContextPosts)
	}
	seen := make(map[string]bool, len(c.Rooms))
	for _, room := range c.Rooms {
		if !roomsdomain.ValidName(room.Name) {
			return fmt.Errorf("rooms.rooms name %q: must start with a letter or digit and keep to letters, digits and underscores", room.Name)
		}
		if seen[room.Name] {
			return fmt.Errorf("rooms.rooms: room %q is defined twice", room.Name)
		}
		seen[room.Name] = true
	}
	return nil
}
