package rooms

import (
	"cmp"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	projects "github.com/inference-gateway/cli/internal/projects"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// AgentCatalog lists the agents a thread in projectDir can run as, with the
// descriptions the router reads.
type AgentCatalog func(projectDir string) []agentdomain.SubagentInfo

// Resolve expands the configured rooms into the rooms that exist: "*" becomes
// every non-built-in agent the room's project can run as, a room without a
// project runs in the default project dir (the working directory of the
// process that reads it), and the agents resolve each time the rooms are read.
func Resolve(cfg config.RoomsConfig, agents AgentCatalog) []roomsdomain.Room {
	resolved := make([]roomsdomain.Room, 0, len(cfg.Rooms))
	for _, room := range cfg.Rooms {
		project := cmp.Or(room.Project, projects.Path())
		resolved = append(resolved, roomsdomain.Room{
			Name:    room.Name,
			Project: project,
			Mode:    cmp.Or(room.Mode, config.DefaultRoomsMemberMode),
			Pickup:  cmp.Or(room.Pickup, config.DefaultRoomsPickup),
			Agents:  roomsdomain.ResolveMemberAgents(room.Agents, agents(project)),
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
