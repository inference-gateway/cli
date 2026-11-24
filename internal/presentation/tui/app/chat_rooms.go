package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	autocomplete "github.com/inference-gateway/cli/internal/presentation/tui/autocomplete"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// roomRequestTimeout bounds one rooms request, a post's included, which the
// daemon may route through the decision model first.
const roomRequestTimeout = 2 * time.Minute

// noRoom is the dropdown's value for the plain chat.
const noRoom = ""

const (
	daemonUnreachableHint = "infer daemon is not running - start it with `infer daemon` to use rooms"
	roomShellRefusal      = "Leave the room to run ! commands - pick No room in the Room dropdown"
	roomImageRefusal      = "Rooms take text only - remove the image, or pick No room in the Room dropdown to send it"
)

// roomPickerSpec titles the dropdown that switches the room a chat is in.
var roomPickerSpec = components.PickerSpec{
	Title: "Switch room",
	Hint:  "your messages post to it",
	Keys:  "↑/↓ move · enter switch · esc close",
}

// roomsListedMsg carries the rooms the daemon reported for the dropdown.
type roomsListedMsg struct {
	rooms []roomsdomain.RoomStatus
	err   error
}

// roomJoinFailedMsg reports a room the daemon could not follow.
type roomJoinFailedMsg struct {
	room string
	err  error
}

// roomPostedMsg carries where a post went, or why it did not.
type roomPostedMsg struct {
	room   string
	posted roomsdomain.Posted
	err    error
}

// SetRooms gives the chat the daemon's rooms, which shows the Room indicator.
func (app *ChatApplication) SetRooms(rooms tui.Rooms) {
	app.rooms = rooms
	if isb, ok := app.inputStatusBar.(*components.InputStatusBar); ok && rooms != nil {
		isb.EnableRooms()
	}
}

// conversationArea is what the layout draws above the composer: the room the
// chat is in, or its own conversation.
func (app *ChatApplication) conversationArea() tui.ConversationArea {
	if app.room != noRoom {
		return app.roomView
	}
	return app.conversationView
}

// openRoomPicker asks the daemon for its rooms. The dropdown opens once they
// arrive, or the status says the daemon is not running.
func (app *ChatApplication) openRoomPicker() tea.Cmd {
	if app.rooms == nil {
		return statusCmd(daemonUnreachableHint)
	}
	rooms := app.rooms
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), roomRequestTimeout)
		defer cancel()
		listed, err := rooms.List(ctx)
		return roomsListedMsg{rooms: listed, err: err}
	}
}

// roomRows lists No room first, then every room with its member count.
func roomRows(rooms []roomsdomain.RoomStatus) []components.PickerRow {
	rows := []components.PickerRow{{Value: noRoom, Label: "No room", Detail: "back to this chat"}}
	for _, room := range rooms {
		rows = append(rows, components.PickerRow{
			Value:  room.Name,
			Label:  "# " + room.Name,
			Detail: components.MemberCount(len(room.Members)) + " · " + room.Project,
		})
	}
	return rows
}

// handleRoomPickerKeys interprets keys while the room dropdown is open, which
// holds every key until it closes.
func (app *ChatApplication) handleRoomPickerKeys(keyMsg tea.KeyPressMsg) []tea.Cmd {
	gk := guardKeys
	switch {
	case key.Matches(keyMsg, gk.navUp):
		app.roomPicker.Move(-1)
	case key.Matches(keyMsg, gk.navDown):
		app.roomPicker.Move(1)
	case key.Matches(keyMsg, gk.cancel):
		app.roomPicker.Close()
	case key.Matches(keyMsg, gk.confirm):
		room := app.roomPicker.Selected()
		app.roomPicker.Close()
		return []tea.Cmd{app.switchRoom(room)}
	}
	return nil
}

// switchRoom moves the chat into room, or back to the plain chat for noRoom.
func (app *ChatApplication) switchRoom(room string) tea.Cmd {
	switch room {
	case app.room:
		return nil
	case noRoom:
		return app.leaveRoom()
	}
	return app.joinRoom(room)
}

// joinRoom leaves the room the chat is in, if any, and joins room, whose
// snapshot then shows it.
func (app *ChatApplication) joinRoom(room string) tea.Cmd {
	rooms, leaving := app.rooms, app.room
	return func() tea.Msg {
		if leaving != noRoom {
			if err := rooms.Leave(leaving); err != nil {
				logger.Debug("could not leave a room", "room", leaving, "error", err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), roomRequestTimeout)
		defer cancel()
		snapshot, err := rooms.Join(ctx, room)
		if err != nil {
			return roomJoinFailedMsg{room: room, err: err}
		}
		return tui.RoomSnapshotEvent{Snapshot: snapshot}
	}
}

// leaveRoom brings the plain chat back, untouched, and stops following the
// room.
func (app *ChatApplication) leaveRoom() tea.Cmd {
	rooms, room := app.rooms, app.room
	app.closeRoom()
	leave := func() tea.Msg {
		if err := rooms.Leave(room); err != nil {
			logger.Debug("could not leave a room", "room", room, "error", err)
		}
		return nil
	}
	return tea.Batch(leave, statusCmd("Left #"+room+" - back in your chat"))
}

// handleRoomEvents drives the room dropdown, the room on screen and posting.
func (app *ChatApplication) handleRoomEvents(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case roomsListedMsg:
		return app.showRoomPicker(m)
	case tui.RoomSnapshotEvent:
		return app.showRoom(m.Snapshot)
	case roomJoinFailedMsg:
		app.closeRoom()
		return errorCmd(fmt.Sprintf("Could not join #%s: %v", m.room, m.err), false)
	case tui.RoomPostEvent:
		if m.Room == app.room {
			app.roomView.AddPost(m.Post)
		}
	case tui.RoomActivityEvent:
		if m.Room == app.room {
			app.roomView.SetActivity(m.Member, m.Activity)
		}
	case tui.RoomClearedEvent:
		if m.Room == app.room {
			app.roomView.ClearPosts()
			return statusCmd("#" + m.Room + " was cleared")
		}
	case roomPostedMsg:
		return roomPostedStatus(m)
	}
	return nil
}

func (app *ChatApplication) showRoomPicker(msg roomsListedMsg) tea.Cmd {
	if msg.err != nil {
		logger.Debug("could not list the daemon's rooms", "error", msg.err)
		return statusCmd(daemonUnreachableHint)
	}
	app.roomPicker.Open(roomPickerSpec, roomRows(msg.rooms), app.room)
	return nil
}

// showRoom puts snapshot's room on screen. Entering it also points the
// composer at the room: its trigger table, placeholder and the indicator.
func (app *ChatApplication) showRoom(snapshot roomsdomain.Snapshot) tea.Cmd {
	entering := app.room != snapshot.Room.Name
	app.room = snapshot.Room.Name
	app.roomView.Show(snapshot)
	if !entering {
		return nil
	}
	app.stopViewingJob()
	app.useComposerContext(config.ComposerContextRoom)
	app.inputView.SetPlaceholder("Message #" + app.room)
	if isb, ok := app.inputStatusBar.(*components.InputStatusBar); ok {
		isb.SetRoom(app.room)
	}
	return statusCmd("You're in #" + app.room + " - pick No room in the Room dropdown to get back")
}

// closeRoom takes the room off screen and points the composer back at the
// chat. The chat's conversation stayed current underneath.
func (app *ChatApplication) closeRoom() {
	if app.room == noRoom {
		return
	}
	app.room = noRoom
	app.useComposerContext(config.ComposerContextChat)
	app.inputView.SetPlaceholder(components.DefaultInputPlaceholder)
	if isb, ok := app.inputStatusBar.(*components.InputStatusBar); ok {
		isb.SetRoom(noRoom)
	}
}

// useComposerContext switches the composer's trigger menus to the table of
// composer.yaml's context.
func (app *ChatApplication) useComposerContext(context string) {
	if ac, ok := app.autocomplete.(*autocomplete.Autocomplete); ok && app.config != nil {
		ac.SetTriggerSources(app.config.Composer.Triggers[context])
	}
}

// postToRoom posts the composer's text to the room. A shell command stays in
// the composer, since it would run in the chat's own agent, not the room, and
// so does a message with an image, which a room cannot carry.
func (app *ChatApplication) postToRoom(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "!") {
		return statusCmd(roomShellRefusal)
	}
	if len(app.inputView.GetImageAttachments()) > 0 {
		return statusCmd(roomImageRefusal)
	}
	if err := app.inputView.AddToHistory(text); err != nil {
		logger.Error("failed to add input to history", "error", err)
	}
	app.inputView.ClearInput()
	rooms, room := app.rooms, app.room
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), roomRequestTimeout)
		defer cancel()
		posted, err := rooms.Post(ctx, room, text)
		return roomPostedMsg{room: room, posted: posted, err: err}
	}
}

// roomPostedStatus says where a post went: the router's notices, or the
// members it reached.
func roomPostedStatus(msg roomPostedMsg) tea.Cmd {
	if msg.err != nil {
		return errorCmd(fmt.Sprintf("Could not post to #%s: %v", msg.room, msg.err), false)
	}
	if len(msg.posted.Notices) > 0 {
		return statusCmd(strings.Join(msg.posted.Notices, " "))
	}
	return statusCmd(msg.posted.RouteSummary())
}

// scrollsRoom reports whether msg scrolls the room on screen rather than the
// chat's conversation underneath.
func (app *ChatApplication) scrollsRoom(msg tea.Msg) bool {
	if app.room == noRoom {
		return false
	}
	switch msg.(type) {
	case tui.ScrollRequestEvent, tea.MouseWheelMsg:
		return true
	}
	return false
}
