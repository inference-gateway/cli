package rooms

import (
	"cmp"

	config "github.com/inference-gateway/cli/config"
	projects "github.com/inference-gateway/cli/internal/projects"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// Resolve expands the configured rooms into the rooms that exist: "*" becomes
// every agent the registry knows, a room without a project runs in the default
// project dir (the working directory of the process that reads it), and the
// agents resolve each time the rooms are read rather than once at load.
func Resolve(cfg config.RoomsConfig, known []string) []roomsdomain.Room {
	resolved := make([]roomsdomain.Room, 0, len(cfg.Rooms))
	for _, room := range cfg.Rooms {
		resolved = append(resolved, roomsdomain.Room{
			Name:    room.Name,
			Project: cmp.Or(room.Project, projects.Path()),
			Agents:  roomsdomain.ResolveMemberAgents(room.Agents, known),
		})
	}
	return resolved
}

// Find returns the room whose name matches, false when no room does.
func Find(resolved []roomsdomain.Room, name string) (roomsdomain.Room, bool) {
	for _, room := range resolved {
		if room.Name == name {
			return room, true
		}
	}
	return roomsdomain.Room{}, false
}
