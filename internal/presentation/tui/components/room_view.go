package components

import (
	"fmt"
	"strings"
	"time"

	viewport "charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	markdown "github.com/inference-gateway/cli/internal/presentation/tui/markdown"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// groupWindow is how soon an author's next message must follow to share the
// author line of the one before, as a Slack channel groups them.
const groupWindow = 5 * time.Minute

const (
	roomTimeLayout       = "15:04"
	roomViewGutter       = 2
	roomWheelDelta       = 3
	roomUserName         = "you"
	conversationScrollID = "conversation"
)

// activityPhrases say what a busy member is doing, under the posts.
var activityPhrases = map[roomsdomain.Activity]string{
	roomsdomain.ActivityRunning: "is working…",
	roomsdomain.ActivityWaiting: "is waiting for approval",
}

// presence is how the header shows what a member does: a dot in a theme color
// and a word, as a Slack sidebar shows who is around.
type presence struct {
	dot, label, color string
}

var presences = map[roomsdomain.Activity]presence{
	roomsdomain.ActivityIdle:    {dot: "○", label: "idle", color: "dim"},
	roomsdomain.ActivityRunning: {dot: "●", label: "working", color: "success"},
	roomsdomain.ActivityWaiting: {dot: "◐", label: "waiting for approval", color: "warning"},
}

// RoomView is the chat's conversation area inside a room, drawn like a Slack
// channel: a header naming the room and what each member does, each message
// under its author and time, joins and room notes as quiet lines, and who is
// busy below the posts. It stays on the newest post unless the user scrolled up.
type RoomView struct {
	styleProvider *styles.Provider
	markdown      *markdown.Renderer
	viewport      viewport.Model
	room          roomsdomain.RoomStatus
	posts         []roomsdomain.Post
	activity      map[string]roomsdomain.Activity
	width         int
	height        int
	banner        string
}

// NewRoomView creates an empty room view.
func NewRoomView(styleProvider *styles.Provider) *RoomView {
	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.KeyMap = viewport.KeyMap{}
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = roomWheelDelta
	view := &RoomView{styleProvider: styleProvider, viewport: vp, width: 80, activity: map[string]roomsdomain.Activity{}}
	if themeService := styleProvider.GetThemeService(); themeService != nil {
		view.markdown = markdown.NewRenderer(themeService, 80)
	}
	return view
}

// Show replaces the room on screen with snapshot: the room, its log so far
// and what each member is doing.
func (v *RoomView) Show(snapshot roomsdomain.Snapshot) {
	v.room = snapshot.Room
	v.posts = snapshot.Posts
	v.activity = map[string]roomsdomain.Activity{}
	for _, member := range snapshot.Room.Members {
		v.activity[member.Agent] = member.Activity
	}
	v.refresh()
}

// Room is the name of the room on screen.
func (v *RoomView) Room() string { return v.room.Name }

// AddPost shows a new post of the room.
func (v *RoomView) AddPost(post roomsdomain.Post) {
	v.posts = append(v.posts, post)
	v.refresh()
}

// ClearPosts empties the room on screen once its log was cleared.
func (v *RoomView) ClearPosts() {
	v.posts = nil
	v.refresh()
}

// SetActivity shows what member is doing now.
func (v *RoomView) SetActivity(member string, activity roomsdomain.Activity) {
	v.activity[member] = activity
	v.refresh()
}

// SetWidth sets the width of the conversation area.
func (v *RoomView) SetWidth(width int) {
	v.width = width
	v.viewport.SetWidth(width)
	if v.markdown != nil {
		v.markdown.SetWidth(max(width-roomViewGutter, 0))
	}
	v.refresh()
}

// SetHeight sets the height of the conversation area.
func (v *RoomView) SetHeight(height int) {
	v.height = height
	v.fitViewport()
}

// Render draws the pinned header and the visible part of the posts.
func (v *RoomView) Render() string {
	if v.banner == "" {
		return v.viewport.View()
	}
	return v.banner + "\n" + v.viewport.View()
}

// fitViewport gives the posts every row the pinned header leaves.
func (v *RoomView) fitViewport() {
	atBottom := v.viewport.AtBottom()
	chrome := 0
	if v.banner != "" {
		chrome = lipgloss.Height(v.banner)
	}
	v.viewport.SetHeight(max(v.height-chrome, 1))
	if atBottom {
		v.viewport.GotoBottom()
	}
}

// Update scrolls the room on the chat's scroll keys and the mouse wheel.
func (v *RoomView) Update(msg tea.Msg) (*RoomView, tea.Cmd) {
	if scroll, ok := msg.(tui.ScrollRequestEvent); ok {
		v.scroll(scroll)
		return v, nil
	}
	var cmd tea.Cmd
	v.viewport, cmd = v.viewport.Update(msg)
	return v, cmd
}

func (v *RoomView) scroll(request tui.ScrollRequestEvent) {
	if request.ComponentID != conversationScrollID {
		return
	}
	switch request.Direction {
	case tui.ScrollUp:
		v.viewport.ScrollUp(request.Amount)
	case tui.ScrollDown:
		v.viewport.ScrollDown(request.Amount)
	case tui.ScrollToTop:
		v.viewport.GotoTop()
	case tui.ScrollToBottom:
		v.viewport.GotoBottom()
	}
}

// refresh redraws the room, staying on the newest post when the user was.
func (v *RoomView) refresh() {
	v.banner = ""
	if v.room.Name != "" {
		v.banner = v.header() + "\n" + v.styleProvider.RenderSeparator(v.width, "─")
	}
	atBottom := v.viewport.AtBottom()
	v.viewport.SetContent(v.content())
	v.fitViewport()
	if atBottom {
		v.viewport.GotoBottom()
	}
}

func (v *RoomView) content() string {
	if v.room.Name == "" {
		return ""
	}
	var lines []string
	var previous *roomsdomain.Post
	for i := range v.posts {
		post := v.posts[i]
		if !continues(previous, post) {
			lines = append(lines, "")
		}
		lines = append(lines, v.renderPost(post, previous))
		previous = &v.posts[i]
	}
	if busy := v.busyLine(); busy != "" {
		lines = append(lines, "", busy)
	}
	return strings.Join(lines, "\n")
}

// header names the room, Slack style, with its members.
func (v *RoomView) header() string {
	members := make([]string, 0, len(v.room.Members))
	for _, member := range v.room.Members {
		members = append(members, v.memberPresence(member.Agent))
	}
	accent := v.styleProvider.GetThemeColor("accent")
	name := v.styleProvider.RenderStyledText("# "+v.room.Name, styles.StyleOptions{Foreground: accent, Bold: true})
	line := name + v.styleProvider.RenderDimText(" · "+MemberCount(len(members))) + "  " + strings.Join(members, v.styleProvider.RenderDimText(" · "))
	return lipgloss.NewStyle().Width(max(v.width, 1)).Render(line)
}

// memberPresence is a member's name after its presence dot, then what it does.
func (v *RoomView) memberPresence(agent string) string {
	p, ok := presences[v.activity[agent]]
	if !ok {
		p = presences[roomsdomain.ActivityIdle]
	}
	dot := v.styleProvider.RenderWithColor(p.dot, v.styleProvider.GetThemeColor(p.color))
	return dot + " " + agent + " " + v.styleProvider.RenderDimText(p.label)
}

// MemberCount says how many members a room has, as "1 member" or "3 members".
func MemberCount(n int) string {
	if n == 1 {
		return "1 member"
	}
	return fmt.Sprintf("%d members", n)
}

// renderPost draws one post: a join or a room note as a quiet line, a message
// under its author and time unless it continues the author's last message.
func (v *RoomView) renderPost(post roomsdomain.Post, previous *roomsdomain.Post) string {
	quiet := styles.StyleOptions{Foreground: v.styleProvider.GetThemeColor("dim"), Italic: true}
	switch {
	case post.Kind == roomsdomain.PostKindJoin:
		return v.styleProvider.RenderStyledText(fmt.Sprintf("→ %s joined #%s", authorName(post.Author), v.room.Name), quiet)
	case post.Author == roomsdomain.RoomAuthor:
		return v.styleProvider.RenderStyledText(post.Text, quiet)
	}
	body := v.renderBody(post)
	if continues(previous, post) {
		return body
	}
	return v.authorLine(post) + "\n" + body
}

// authorLine is the bold author name, the user as "you", and the post's time.
func (v *RoomView) authorLine(post roomsdomain.Post) string {
	color := v.styleProvider.GetThemeColor("accent")
	if post.Author == roomsdomain.UserAuthor {
		color = v.styleProvider.GetThemeColor("user")
	}
	name := v.styleProvider.RenderStyledText(authorName(post.Author), styles.StyleOptions{Foreground: color, Bold: true})
	return name + "  " + v.styleProvider.RenderDimText(post.Time.Local().Format(roomTimeLayout))
}

// renderBody renders a member's message as markdown and the user's as typed,
// with their mentions highlighted, both wrapped to the room's width.
func (v *RoomView) renderBody(post roomsdomain.Post) string {
	width := max(v.width-roomViewGutter, 1)
	if post.Author != roomsdomain.UserAuthor && v.markdown != nil {
		return v.markdown.Render(post.Text)
	}
	text := post.Text
	accent := v.styleProvider.GetThemeColor("accent")
	for _, mention := range post.Mentions {
		text = strings.ReplaceAll(text, "@"+mention, v.styleProvider.RenderWithColorAndBold("@"+mention, accent))
	}
	return lipgloss.NewStyle().Width(width).Render(text)
}

// busyLine says which members are working or waiting, in the room's order.
func (v *RoomView) busyLine() string {
	var busy []string
	for _, member := range v.room.Members {
		if phrase, ok := activityPhrases[v.activity[member.Agent]]; ok {
			busy = append(busy, member.Agent+" "+phrase)
		}
	}
	if len(busy) == 0 {
		return ""
	}
	return v.styleProvider.RenderStyledText(strings.Join(busy, " · "), styles.StyleOptions{Foreground: v.styleProvider.GetThemeColor("dim"), Italic: true})
}

// continues reports whether post is a message that shares the author line of
// the message before it: same author, soon after.
func continues(previous *roomsdomain.Post, post roomsdomain.Post) bool {
	return previous != nil && isMessage(*previous) && isMessage(post) &&
		previous.Author == post.Author && post.Time.Sub(previous.Time) <= groupWindow
}

func isMessage(post roomsdomain.Post) bool {
	return post.Kind == "" && post.Author != roomsdomain.RoomAuthor
}

func authorName(author string) string {
	if author == roomsdomain.UserAuthor {
		return roomUserName
	}
	return author
}
