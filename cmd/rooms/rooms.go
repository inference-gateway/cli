package rooms

import (
	"fmt"
	"strings"
	"time"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	roomscontext "github.com/inference-gateway/cli/internal/rooms"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
	roomsinfra "github.com/inference-gateway/cli/internal/rooms/infrastructure"
)

type command struct {
	state    *runtime.State
	renderer *output.Renderer
}

// NewCommand constructs the rooms command tree.
func NewCommand(state *runtime.State, renderer *output.Renderer) *cobra.Command {
	cmd := &command{state: state, renderer: renderer}
	roomsCmd := &cobra.Command{
		Use:   "rooms",
		Short: "Inspect rooms: named groups of agents sharing one room log",
		Long: `Inspect the rooms defined in rooms.yaml and their logs.

A room is a named group of agents the user talks to in one project. Every
agent in a room keeps its own session with the deterministic id
room-<room>-<agent>, and every post the room sees is appended to one
JSONL log per room.`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List the rooms with their resolved members",
		Long:  `List every room in rooms.yaml with the members resolved now: "*" becomes every agent that exists, built-ins included.`,
		RunE:  cmd.listRooms,
	}
	showCmd := &cobra.Command{
		Use:   "show <room>",
		Short: "Show one room and its log",
		Long:  `Show one room's members, its routing settings and the posts of its log.`,
		Args:  cobra.ExactArgs(1),
		RunE:  cmd.showRoom,
	}
	roomsCmd.AddCommand(listCmd, showCmd)

	output.AddFormatFlag(listCmd, "text", "json")
	output.AddFormatFlag(showCmd, "text", "json")
	return roomsCmd
}

// roomView is one room as the rooms commands print it: the resolved members
// with the session id each one keeps.
type roomView struct {
	Name    string       `json:"name"`
	Project string       `json:"project"`
	Members []memberView `json:"members"`
}

// memberView is one room member.
type memberView struct {
	Agent     string `json:"agent"`
	SessionID string `json:"session_id"`
}

// showView is what infer rooms show prints: the room plus the router settings
// that govern it and the posts of its log.
type showView struct {
	Room   roomView                 `json:"room"`
	Router config.RoomsRouterConfig `json:"router"`
	Posts  []roomsdomain.Post       `json:"posts"`
}

// resolvedRooms resolves the configured rooms against every agent the tool
// registry knows, the same list `infer agents list` prints.
func (c *command) resolvedRooms() []roomsdomain.Room {
	return roomscontext.Resolve(c.state.Config().Rooms, c.agentNames())
}

// agentNames lists the Markdown agents the tool registry holds: the built-ins
// and the agent files of the agent dirs.
func (c *command) agentNames() []string {
	infos := container.NewServiceContainer(c.state.Config()).GetToolRegistry().MarkdownSubagents()
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	return names
}

func (c *command) listRooms(cmd *cobra.Command, _ []string) error {
	views := roomViews(c.resolvedRooms())

	if format, _ := cmd.Flags().GetString("format"); format == "json" {
		return output.PrintJSON(cmd.OutOrStdout(), views)
	}

	w := cmd.OutOrStdout()
	if len(views) == 0 {
		fmt.Fprintln(w, "No rooms configured. Add one to rooms.yaml.")
		return nil
	}
	fmt.Fprintln(w, c.renderer.Title(fmt.Sprintf("Rooms (%d)", len(views))))
	fmt.Fprintln(w)
	table := c.renderer.NewAgentsListTable("Name", "Project", "Members")
	for _, view := range views {
		members := make([]string, 0, len(view.Members))
		for _, member := range view.Members {
			members = append(members, member.Agent)
		}
		table.Row(view.Name, view.Project, strings.Join(members, ", "))
	}
	fmt.Fprintln(w, table.Render())
	fmt.Fprintln(w)
	return nil
}

func (c *command) showRoom(cmd *cobra.Command, args []string) error {
	name := args[0]
	resolved := c.resolvedRooms()
	room, found := roomscontext.Find(resolved, name)
	if !found {
		return fmt.Errorf("room %q not found; known rooms: %v", name, roomNames(resolved))
	}

	posts, err := roomsinfra.NewJSONLLog().Read(room)
	if err != nil {
		return err
	}

	view := showView{
		Room:   roomViewOf(room),
		Router: c.state.Config().Rooms.Router,
		Posts:  posts,
	}

	if format, _ := cmd.Flags().GetString("format"); format == "json" {
		return output.PrintJSON(cmd.OutOrStdout(), view)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintln(w, c.renderer.Title("Room: "+view.Room.Name))
	fmt.Fprintln(w)
	fmt.Fprintln(w, c.renderer.Field("Project", view.Room.Project))
	fmt.Fprintln(w, c.renderer.Field("Model", valueOr(view.Router.Model)))
	fmt.Fprintln(w, c.renderer.Field("Min confidence", fmt.Sprintf("%g", view.Router.MinConfidence)))
	fmt.Fprintln(w, c.renderer.Field("Fallback", valueOr(view.Router.Fallback)))
	fmt.Fprintln(w)

	fmt.Fprintln(w, c.renderer.Title(fmt.Sprintf("Members (%d)", len(view.Room.Members))))
	memberTable := c.renderer.NewAgentsListTable("Agent", "Session ID")
	for _, member := range view.Room.Members {
		memberTable.Row(member.Agent, member.SessionID)
	}
	fmt.Fprintln(w, memberTable.Render())
	fmt.Fprintln(w)

	fmt.Fprintln(w, c.renderer.Title(fmt.Sprintf("Log (%d posts)", len(view.Posts))))
	if len(view.Posts) == 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "No posts yet.")
		return nil
	}
	postTable := c.renderer.NewListTable("Time", "Author", "Routing", "Text")
	for _, post := range view.Posts {
		postTable.Row(post.Time.Format(time.DateTime), post.Author, routingOf(post), oneLine(post.Text))
	}
	fmt.Fprintln(w, postTable.Render())
	return nil
}

func roomViews(resolved []roomsdomain.Room) []roomView {
	views := make([]roomView, 0, len(resolved))
	for _, room := range resolved {
		views = append(views, roomViewOf(room))
	}
	return views
}

func roomViewOf(room roomsdomain.Room) roomView {
	members := room.Members()
	views := make([]memberView, 0, len(members))
	for _, member := range members {
		views = append(views, memberView{Agent: member.Agent, SessionID: member.SessionID})
	}
	return roomView{Name: room.Name, Project: room.Project, Members: views}
}

func roomNames(resolved []roomsdomain.Room) []string {
	names := make([]string, 0, len(resolved))
	for _, room := range resolved {
		names = append(names, room.Name)
	}
	return names
}

// routingOf renders who a post was routed to with the router's probability.
func routingOf(post roomsdomain.Post) string {
	if len(post.Routes) == 0 {
		return "-"
	}
	routes := make([]string, 0, len(post.Routes))
	for _, route := range post.Routes {
		routes = append(routes, fmt.Sprintf("%s@%g", route.Member, route.Probability))
	}
	return strings.Join(routes, ", ")
}

// oneLine keeps a post's text to one table row line.
func oneLine(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 72 {
		return text[:72] + "..."
	}
	return text
}

func valueOr(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
