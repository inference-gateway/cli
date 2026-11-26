package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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

// roomClearCommands wipe the chat's own conversation, which sits hidden under
// a room, so inside one they are held back rather than taken for its clear.
var roomClearCommands = []string{"/clear", "/new"}

// roomStopCommand aborts the members at work in the room, every one or the one
// it names. It exists inside a room only, where the "/" menu lists it.
const roomStopCommand = "/stop"

var roomShortcuts = []autocomplete.ShortcutOption{{
	Shortcut:    roomStopCommand,
	Description: "Stop every member working in the room, or one member",
	Usage:       "/stop [member]",
	Kind:        autocomplete.KindCommand,
}}

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

// roomStoppedMsg carries who a /stop stopped, or why it could not.
type roomStoppedMsg struct {
	room, member string
	stopped      []string
	err          error
}

// roomDecidedMsg carries why a decision on a member's call did not land, nil
// when it did.
type roomDecidedMsg struct {
	room, member string
	err          error
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
		return tea.Batch(app.showRoom(m.Snapshot), app.offerRoomCall())
	case roomJoinFailedMsg:
		app.closeRoom()
		return errorCmd(fmt.Sprintf("Could not join #%s: %v", m.room, m.err), false)
	case tui.RoomPostEvent:
		if m.Room == app.room {
			app.roomView.AddPost(m.Post)
			return app.offerRoomCall()
		}
	case tui.RoomApprovalAnsweredEvent:
		return app.answerRoomCall(m)
	case tui.RoomActivityEvent:
		if m.Room == app.room {
			app.roomView.SetActivity(m.Member, m.Activity)
		}
		if m.Room == app.room && m.Mode != "" {
			app.modeIndicator.SetRoom(app.room, m.Mode)
		}
	case tui.RoomClearedEvent:
		if m.Room == app.room {
			app.roomView.ClearPosts()
			return tea.Batch(app.offerRoomCall(), statusCmd("#"+m.Room+" was cleared"))
		}
	case roomPostedMsg:
		return roomPostedStatus(m)
	case roomStoppedMsg:
		return roomStoppedStatus(m)
	case roomDecidedMsg:
		if m.err != nil {
			return tea.Batch(errorCmd(fmt.Sprintf("Could not decide %s's call in #%s: %v", m.member, m.room, m.err), false), app.offerRoomCall())
		}
	}
	return nil
}

// refuseRoomModeCycle says a room's mode is not switched from the chat: its
// members run in the mode rooms.yaml sets, which the mode indicator shows.
func (app *ChatApplication) refuseRoomModeCycle() tea.Cmd {
	return statusCmd(fmt.Sprintf("#%s's mode is set in rooms.yaml (rooms.%s.mode)", app.room, app.room))
}

// offerRoomCall shows the newest call the room on screen waits on in the
// approval window every tool approval uses, and takes a call that closed off
// it. The chat's own approval keeps the window until it is answered.
func (app *ChatApplication) offerRoomCall() tea.Cmd {
	post, open := app.roomView.NewestOpenApproval()
	open = open && app.room != noRoom
	if open && app.roomCall.Approval != nil && post.Approval.ID == app.roomCall.Approval.ID {
		return nil
	}
	if state := app.stateManager.GetApprovalUIState(); state != nil && state.Room == "" {
		return nil
	}
	app.withdrawRoomCall()
	if !open {
		return nil
	}
	app.roomCall = post
	app.stateManager.SetupApprovalUIState(&sdk.ChatCompletionMessageToolCall{
		ID:       post.Approval.ID,
		Type:     sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{Name: post.Approval.Tool, Arguments: post.Approval.Arguments},
	}, nil)
	if state := app.stateManager.GetApprovalUIState(); state != nil {
		state.Room = app.room
		state.Context = roomCallContext(app.room, post)
	}
	return app.approvalBoxView.Begin()
}

// withdrawRoomCall takes the room's call off the approval window, which stays
// open in the room.
func (app *ChatApplication) withdrawRoomCall() {
	if app.roomCall.Approval == nil {
		return
	}
	app.roomCall = roomsdomain.Post{}
	if state := app.stateManager.GetApprovalUIState(); state != nil && state.Room != "" {
		app.stateManager.ClearApprovalUIState()
	}
}

// roomCallContext says who in which room asks, above the note the call came
// with, such as the judge's reason and the member's justification.
func roomCallContext(room string, post roomsdomain.Post) string {
	asks := fmt.Sprintf("%s in #%s asks to run this.", post.Author, room)
	if post.Text == "" {
		return asks
	}
	return asks + "\n" + post.Text
}

// answerRoomCall sends the approval window's answer on the room's call to the
// room, which posts the decision.
func (app *ChatApplication) answerRoomCall(answer tui.RoomApprovalAnsweredEvent) tea.Cmd {
	post := app.roomCall
	if post.Approval == nil || post.Approval.ID != answer.ToolCallID {
		return nil
	}
	app.withdrawRoomCall()
	decision := roomsdomain.DecisionReject
	if answer.Action == agentdomain.ApprovalApprove {
		decision = roomsdomain.DecisionApprove
	}
	return app.decideRoomApproval(post, decision)
}

// decideRoomApproval sends the decision on the call of an approval post. The
// room's line about it arrives as a post, and a refusal as an error status.
func (app *ChatApplication) decideRoomApproval(post roomsdomain.Post, decision roomsdomain.Decision) tea.Cmd {
	rooms, room := app.rooms, app.room
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), roomRequestTimeout)
		defer cancel()
		err := rooms.Decide(ctx, room, post.Author, post.Approval.ID, decision)
		return roomDecidedMsg{room: room, member: post.Author, err: err}
	}
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
	app.modeIndicator.SetRoom(app.room, snapshot.Room.Mode)
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
	app.withdrawRoomCall()
	app.room = noRoom
	app.modeIndicator.SetRoom(noRoom, "")
	app.useComposerContext(config.ComposerContextChat)
	app.inputView.SetPlaceholder(components.DefaultInputPlaceholder)
	if isb, ok := app.inputStatusBar.(*components.InputStatusBar); ok {
		isb.SetRoom(noRoom)
	}
}

// useComposerContext switches the composer's trigger menus to the table of
// composer.yaml's context.
func (app *ChatApplication) useComposerContext(context string) {
	ac, ok := app.autocomplete.(*autocomplete.Autocomplete)
	if !ok {
		return
	}
	if app.config != nil {
		ac.SetTriggerSources(app.config.Composer.Triggers[context])
	}
	if context == config.ComposerContextRoom {
		ac.SetContextShortcuts(roomShortcuts)
		return
	}
	ac.SetContextShortcuts(nil)
}

// stopsRoom reports whether input, inside a room, is its /stop command.
func (app *ChatApplication) stopsRoom(input string) bool {
	command, _, _ := strings.Cut(input, " ")
	return app.room != noRoom && command == roomStopCommand
}

// stopRoom asks the daemon to stop every member at work in the room, or the
// one input names after /stop, and clears the composer.
func (app *ChatApplication) stopRoom(input string) tea.Cmd {
	_, member, _ := strings.Cut(input, " ")
	member = strings.TrimPrefix(strings.TrimSpace(member), "@")
	app.inputView.ClearInput()
	rooms, room := app.rooms, app.room
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), roomRequestTimeout)
		defer cancel()
		stopped, err := rooms.Stop(ctx, room, member)
		return roomStoppedMsg{room: room, member: member, stopped: stopped, err: err}
	}
}

// roomStoppedStatus says who a /stop stopped, or that nobody was at work.
func roomStoppedStatus(msg roomStoppedMsg) tea.Cmd {
	switch {
	case msg.err != nil:
		return errorCmd(fmt.Sprintf("Could not stop in #%s: %v", msg.room, msg.err), false)
	case len(msg.stopped) > 0:
		return statusCmd(fmt.Sprintf("Stopped %s in #%s", strings.Join(msg.stopped, ", "), msg.room))
	case msg.member != "":
		return statusCmd(fmt.Sprintf("%s is not working in #%s", msg.member, msg.room))
	}
	return statusCmd("Nobody is working in #" + msg.room)
}

// clearsInRoom reports whether input, inside a room, is a command that would
// wipe the chat hidden under it.
func (app *ChatApplication) clearsInRoom(input string) bool {
	command, _, _ := strings.Cut(input, " ")
	return app.room != noRoom && slices.Contains(roomClearCommands, command)
}

// refuseRoomClear says a room is never cleared from the chat, since other
// people read it too, and how to empty it on purpose. Nothing is cleared, the
// chat hidden under the room included.
func (app *ChatApplication) refuseRoomClear() tea.Cmd {
	return statusCmd(fmt.Sprintf("#%s is not cleared from the chat, others read it too - "+
		"run `infer rooms clear %s` to empty it on purpose, or pick No room to clear your own chat", app.room, app.room))
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
