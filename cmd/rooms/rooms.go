package rooms

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	daemonhost "github.com/inference-gateway/cli/internal/daemon"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	roomscontext "github.com/inference-gateway/cli/internal/rooms"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
	roomsinfra "github.com/inference-gateway/cli/internal/rooms/infrastructure"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// postTextWidth is how many columns of a post's text the log table shows.
const postTextWidth = 72

// daemonTimeout bounds asking the daemon for its rooms, after which they
// resolve here.
const daemonTimeout = 5 * time.Second

type command struct {
	state    *runtime.State
	renderer *output.Renderer
}

// NewCommand constructs the rooms command tree.
func NewCommand(state *runtime.State, renderer *output.Renderer) *cobra.Command {
	cmd := &command{state: state, renderer: renderer}
	roomsCmd := &cobra.Command{
		Use:   "rooms",
		Short: "Inspect and post to rooms: named groups of agents sharing one room log",
		Long: `Inspect the rooms defined in rooms.yaml and their logs, and post to them
through the daemon.

A room is a named group of agents the user talks to in one project. Every
agent in a room keeps its own session with the deterministic id
room-<room>-<agent>, and every post the room sees is appended to one
JSONL log per room.`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List the rooms with their resolved members",
		Long: `List every room in rooms.yaml with the members resolved now: "*" becomes every agent of yours, the built-ins left out.
A running daemon answers with the rooms it hosts, so the rooms, projects and
logs match what infer rooms post reaches.`,
		RunE: cmd.listRooms,
	}
	showCmd := &cobra.Command{
		Use:   "show <room>",
		Short: "Show one room and its log",
		Long: `Show one room's members, its routing settings and the posts of its log.
A running daemon resolves the room, so the log is the one it writes.`,
		Args: cobra.ExactArgs(1),
		RunE: cmd.showRoom,
	}
	postCmd := &cobra.Command{
		Use:   "post <room> <text>",
		Short: "Post to a room through the daemon and print where the post went",
		Long: `Post to a room as the user. The daemon routes the post to the members it
pings, or asks the router model about a post that pings nobody, logs it, and
delivers it to every routed member as that member's next turn.`,
		Args: cobra.ExactArgs(2),
		RunE: cmd.postRoom,
	}
	clearCmd := &cobra.Command{
		Use:   "clear <room>",
		Short: "Empty a room's log",
		Long: `Empty a room's log, so the room starts over: clients following it empty
their view, and the next time you enter it the members welcome you again.
Each member keeps its own session, room-<room>-<agent>. A running daemon clears
the log it writes, and with no daemon listening the log clears where you run the command.`,
		Args: cobra.ExactArgs(1),
		RunE: cmd.clearRoom,
	}
	roomsCmd.AddCommand(listCmd, showCmd, postCmd, clearCmd)

	output.AddFormatFlag(listCmd, "text", "json")
	output.AddFormatFlag(showCmd, "text", "json")
	output.AddFormatFlag(postCmd, "text", "json")
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

// rooms lists the rooms as the running daemon resolves them, since it hosts
// them and writes their logs, and as this process resolves them when no daemon
// answers.
func (c *command) rooms(ctx context.Context) []roomView {
	views, err := c.daemonRooms(ctx)
	if err != nil {
		logger.Debug("rooms resolve here, the daemon did not answer", "error", err)
		return roomViews(roomscontext.Resolve(c.state.Config().Rooms, tools.ProjectAgents))
	}
	return views
}

func (c *command) daemonRooms(ctx context.Context) ([]roomView, error) {
	ctx, cancel := context.WithTimeout(ctx, daemonTimeout)
	defer cancel()
	client, err := daemonhost.DialLocal(ctx, c.state.Config(), clientKind)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	if err := client.Send(map[string]string{"type": roomsdomain.FrameListRooms}); err != nil {
		return nil, err
	}
	frame, err := client.Next(ctx, roomsdomain.FrameRooms)
	if err != nil {
		return nil, err
	}
	var answer struct {
		Rooms []roomView `json:"rooms"`
	}
	return answer.Rooms, decodeFrame(frame, &answer)
}

func (c *command) listRooms(cmd *cobra.Command, _ []string) error {
	views := c.rooms(cmd.Context())

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
	table := c.renderer.NewListTable("Name", "Project", "Members")
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
	views := c.rooms(cmd.Context())
	i := slices.IndexFunc(views, func(view roomView) bool { return view.Name == name })
	if i < 0 {
		return fmt.Errorf("room %q not found, known rooms: %s", name, strings.Join(roomNames(views), ", "))
	}

	posts, err := roomsinfra.NewJSONLLog().Read(roomsdomain.Room{Name: views[i].Name, Project: views[i].Project})
	if err != nil {
		return err
	}

	view := showView{
		Room:   views[i],
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
	fmt.Fprintln(w, c.renderer.Field("Model", cmp.Or(view.Router.Model, "-")))
	fmt.Fprintln(w, c.renderer.Field("Min confidence", fmt.Sprintf("%g", view.Router.MinConfidence)))
	fmt.Fprintln(w, c.renderer.Field("Fallback", cmp.Or(view.Router.Fallback, "-")))
	fmt.Fprintln(w)

	fmt.Fprintln(w, c.renderer.Title(fmt.Sprintf("Members (%d)", len(view.Room.Members))))
	memberTable := c.renderer.NewListTable("Agent", "Session ID")
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

func roomNames(views []roomView) []string {
	names := make([]string, 0, len(views))
	for _, view := range views {
		names = append(names, view.Name)
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

// oneLine keeps a post's text to one table row line, cut on a character
// boundary.
func oneLine(text string) string {
	return formatting.TruncateText(strings.ReplaceAll(text, "\n", " "), postTextWidth)
}
