package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"
	ansi "github.com/charmbracelet/x/ansi"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	autocomplete "github.com/inference-gateway/cli/internal/presentation/tui/autocomplete"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

var testRoomSnapshot = roomsdomain.Snapshot{
	Room: roomsdomain.RoomStatus{Name: "release", Project: "/proj", Members: []roomsdomain.MemberStatus{
		{Agent: "tester", Activity: roomsdomain.ActivityIdle},
		{Agent: "writer", Activity: roomsdomain.ActivityIdle},
	}},
}

// newRoomTestApp is a chat that reaches rooms through a fake, with a real
// status bar, composer, trigger menus and room view.
func newRoomTestApp(t *testing.T) (*ChatApplication, *tuimocks.FakeRooms) {
	t.Helper()
	cfg := config.DefaultConfig()
	styleProvider := styles.NewProvider(styles.NewThemeProvider())

	modelService := &convmocks.FakeModelService{}
	modelService.GetCurrentModelReturns("test-model")
	statusBar := components.NewInputStatusBar(styleProvider)
	statusBar.SetModelService(modelService)
	statusBar.SetConfig(cfg)
	statusBar.SetWidth(120)

	files := &agentdomainmocks.FakeFileService{}
	files.ListProjectFilesReturns([]string{"cmd/run.sh"}, nil)
	agents := &agentdomainmocks.FakeToolService{}
	agents.ListMarkdownSubagentsReturns([]agentdomain.SubagentInfo{{Name: "runner"}})
	ac := autocomplete.NewAutocomplete(&tuimocks.FakeTheme{}, &tuimocks.FakeShortcutRegistry{})
	ac.SetFileService(files)
	ac.SetToolService(agents)
	ac.SetTriggerSources(cfg.Composer.Triggers[config.ComposerContextChat])

	store := statemanager.NewStore(false)
	app := &ChatApplication{
		config:                  cfg,
		inputStatusBar:          statusBar,
		inputView:               components.NewInputView(modelService),
		autocomplete:            ac,
		stateManager:            store,
		approvalBoxView:         components.NewApprovalBoxView(styleProvider, store, nil),
		conversationView:        components.NewConversationView(styleProvider),
		applicationViewRenderer: components.NewApplicationViewRenderer(styleProvider),
		roomPicker:              components.NewPicker(styleProvider),
		roomView:                components.NewRoomView(styleProvider),
		newSession:              newSessionBinding(cfg.Chat.Keybindings),
		cycleMode:               cycleModeBinding(cfg.Chat.Keybindings),
		modeIndicator:           modeIndicatorOf(styleProvider),
	}
	rooms := &tuimocks.FakeRooms{}
	app.SetRooms(rooms)
	return app, rooms
}

// modeIndicatorOf is a mode indicator over a chat in the standard mode.
func modeIndicatorOf(styleProvider *styles.Provider) *components.ModeIndicator {
	modes := &agentdomainmocks.FakeAgentModeState{}
	modes.GetAgentModeReturns(agentdomain.AgentModeStandard)
	indicator := components.NewModeIndicator(styleProvider)
	indicator.SetStateManager(modes)
	return indicator
}

// playRoomCmd runs cmd the way the program would, feeding every message it
// yields back to the room handlers, and returns those messages.
func playRoomCmd(app *ChatApplication, cmd tea.Cmd) []tea.Msg {
	var played []tea.Msg
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		next := pending[0]
		pending = pending[1:]
		if next == nil {
			continue
		}
		switch msg := next().(type) {
		case nil:
		case tea.BatchMsg:
			pending = append(pending, msg...)
		default:
			played = append(played, msg)
			pending = append(pending, app.handleRoomEvents(msg))
		}
	}
	return played
}

func statusOf(t *testing.T, played []tea.Msg) string {
	t.Helper()
	var messages []string
	for _, msg := range played {
		switch m := msg.(type) {
		case tui.SetStatusEvent:
			messages = append(messages, m.Message)
		case tui.ShowErrorEvent:
			messages = append(messages, m.Error)
		}
	}
	return strings.Join(messages, "\n")
}

// firstMentionRow is the first row the composer's @ menu offers.
func firstMentionRow(app *ChatApplication) string {
	ac := app.autocomplete.(*autocomplete.Autocomplete)
	ac.Update("@", 1)
	return ac.GetSelectedShortcut()
}

func statusBarText(app *ChatApplication) string {
	return ansi.Strip(app.inputStatusBar.Render())
}

func enterTestRoom(t *testing.T, app *ChatApplication, rooms *tuimocks.FakeRooms) {
	t.Helper()
	rooms.JoinReturns(testRoomSnapshot, nil)
	playRoomCmd(app, app.switchRoom("release"))
	require.Equal(t, "release", app.room)
}

func TestTheRoomDropdownListsNoRoomThenEveryRoom(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	rooms.ListReturns([]roomsdomain.RoomStatus{testRoomSnapshot.Room}, nil)

	require.Contains(t, statusBarText(app), "Room: -")
	playRoomCmd(app, app.activateIndicator(tui.StatusIndicatorActionRoomSelection)[0])

	require.True(t, app.roomPicker.IsOpen())
	require.Equal(t, noRoom, app.roomPicker.Selected(), "the plain chat is the current choice")
	dropdown := ansi.Strip(app.roomPicker.Render())
	require.Less(t, strings.Index(dropdown, "No room"), strings.Index(dropdown, "# release"))
	require.Contains(t, dropdown, "2 members · /proj")
}

func TestAnUnreachableDaemonLeavesTheChatAsItIs(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	rooms.ListReturns(nil, errors.New("dial tcp: connection refused"))

	played := playRoomCmd(app, app.openRoomPicker())

	require.Contains(t, statusOf(t, played), "infer daemon is not running")
	require.False(t, app.roomPicker.IsOpen())
	require.Equal(t, noRoom, app.room)
}

// TestEnteringAndLeavingARoom pins the switch both ways: inside, the room is
// on screen, the indicator names it and the composer uses the room's trigger
// table. No room restores the chat's conversation and table.
func TestEnteringAndLeavingARoom(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	require.Equal(t, "@cmd/run.sh", firstMentionRow(app), "the chat table lists files first")

	enterTestRoom(t, app, rooms)
	_, joined := rooms.JoinArgsForCall(0)
	require.Equal(t, "release", joined)
	require.Same(t, app.roomView, app.conversationArea())
	require.Contains(t, statusBarText(app), "Room: #release")
	require.Equal(t, "@runner", firstMentionRow(app), "the room table lists agents first")

	played := playRoomCmd(app, app.switchRoom(noRoom))
	require.Contains(t, statusOf(t, played), "Left #release")
	require.Equal(t, 1, rooms.LeaveCallCount())
	require.Equal(t, "release", rooms.LeaveArgsForCall(0))
	require.Equal(t, app.conversationView, app.conversationArea())
	require.Contains(t, statusBarText(app), "Room: -")
	require.Equal(t, "@cmd/run.sh", firstMentionRow(app))
}

func TestAFailedJoinKeepsThePlainChat(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	rooms.JoinReturns(roomsdomain.Snapshot{}, errors.New("room \"release\" not found"))

	played := playRoomCmd(app, app.switchRoom("release"))

	require.Contains(t, statusOf(t, played), "Could not join #release")
	require.Equal(t, noRoom, app.room)
}

func TestTheComposerPostsToTheRoom(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	enterTestRoom(t, app, rooms)
	rooms.PostReturns(roomsdomain.Posted{Room: "release", Post: roomsdomain.Post{
		Mentions: []string{roomsdomain.EveryoneMention},
		Routes:   []roomsdomain.Route{{Member: "tester", Probability: 1}, {Member: "writer", Probability: 1}},
	}}, nil)

	app.inputView.SetText("hi @all")
	played := playRoomCmd(app, app.SendMessage())

	require.Equal(t, 1, rooms.PostCallCount())
	_, room, text := rooms.PostArgsForCall(0)
	require.Equal(t, "release", room)
	require.Equal(t, "hi @all", text)
	require.Empty(t, app.inputView.GetInput())
	require.Contains(t, statusOf(t, played), "Routed to tester (pinged), writer (pinged).")
}

func TestShellCommandsStayInTheComposerInARoom(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	enterTestRoom(t, app, rooms)

	app.inputView.SetText("!ls")
	played := playRoomCmd(app, app.SendMessage())

	require.Zero(t, rooms.PostCallCount())
	require.Equal(t, "!ls", app.inputView.GetInput())
	require.Contains(t, statusOf(t, played), "Leave the room to run ! commands")
}

func TestImagesStayInTheComposerInARoom(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	enterTestRoom(t, app, rooms)

	app.inputView.SetText("@tester why does this fail?")
	app.inputView.AddImageAttachment(agentdomain.ImageAttachment{Data: "aGk=", MimeType: "image/png"})
	typed := app.inputView.GetInput()
	played := playRoomCmd(app, app.SendMessage())

	require.Zero(t, rooms.PostCallCount())
	require.Len(t, app.inputView.GetImageAttachments(), 1, "the image is kept, not dropped")
	require.Equal(t, typed, app.inputView.GetInput())
	require.Contains(t, statusOf(t, played), "Rooms take text only")
}

// TestClearingIsHeldBackInARoom pins that the chat's clear never acts inside a
// room, which other people read too: the key and the commands say how to
// empty the room on purpose and leave the composer as it was.
func TestClearingIsHeldBackInARoom(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	enterTestRoom(t, app, rooms)

	cmds := app.handleChatViewKeyPress(tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})
	require.Contains(t, statusOf(t, playRoomCmd(app, tea.Batch(cmds...))), "run `infer rooms clear release` to empty it on purpose")

	for _, command := range []string{"/clear", "/new"} {
		app.inputView.SetText(command)
		require.Contains(t, statusOf(t, playRoomCmd(app, app.SendMessage())), "#release is not cleared from the chat")
		require.Equal(t, command, app.inputView.GetInput(), "the command stays in the composer")
	}
	require.Zero(t, rooms.PostCallCount())
}

// TestARoomShowsItsOwnModeAndHoldsBackShiftTab pins that inside a room the
// mode indicator names the mode rooms.yaml runs its members in, follows the
// mode a member's turn reports, shift+tab switches nothing, and No room shows
// the chat's own mode again.
func TestARoomShowsItsOwnModeAndHoldsBackShiftTab(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	require.Empty(t, ansi.Strip(app.modeIndicator.Text()), "the chat's standard mode has no label")

	rooms.JoinReturns(roomsdomain.Snapshot{Room: roomsdomain.RoomStatus{Name: "release"}}, nil)
	playRoomCmd(app, app.switchRoom("release"))
	require.Equal(t, "#release", ansi.Strip(app.modeIndicator.Text()), "a daemon that reports no mode is not shown as standard")
	playRoomCmd(app, app.switchRoom(noRoom))

	rooms.JoinReturns(roomsdomain.Snapshot{Room: roomsdomain.RoomStatus{Name: "release", Mode: "auto-with-judge"}}, nil)
	playRoomCmd(app, app.switchRoom("release"))
	require.Equal(t, "#release ▸ AUTO+JUDGE", ansi.Strip(app.modeIndicator.Text()))

	cmds := app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	require.Equal(t, "#release's mode is set in rooms.yaml (rooms.release.mode)", statusOf(t, playRoomCmd(app, tea.Batch(cmds...))))
	require.Equal(t, "#release ▸ AUTO+JUDGE", ansi.Strip(app.modeIndicator.Text()), "shift+tab switches nothing in a room")

	app.handleRoomEvents(tui.RoomActivityEvent{Room: "release", Member: "tester", Activity: roomsdomain.ActivityRunning, Mode: "standard"})
	require.Equal(t, "#release ▸ STANDARD", ansi.Strip(app.modeIndicator.Text()), "a turn reports the mode rooms.yaml has now")

	playRoomCmd(app, app.switchRoom(noRoom))
	require.Empty(t, ansi.Strip(app.modeIndicator.Text()), "No room shows the chat's own mode again")
}

// askedCall is a member's call waiting in the test room after the judge
// rejected it.
func askedCall(postID, callID string) roomsdomain.Post {
	return roomsdomain.Post{
		ID: postID, Author: "tester", Kind: roomsdomain.PostKindApproval, Text: "Rejected by judge: tests were not asked for",
		Approval: &roomsdomain.Approval{ID: callID, Tool: "Bash", Arguments: `{"command":"go test ./..."}`}, Time: time.Now(),
	}
}

// answerInApprovalWindow moves the approval window's choice right rights times,
// confirms it and plays what the answer leads to.
func answerInApprovalWindow(t *testing.T, app *ChatApplication, rights int) []tea.Msg {
	t.Helper()
	for range rights {
		_ = app.approvalBoxView.Forward(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	cmd := app.approvalBoxView.Forward(tea.KeyPressMsg{Code: tea.KeyEnter})
	for cmd != nil {
		msg := cmd()
		if answer, ok := msg.(tui.RoomApprovalAnsweredEvent); ok {
			return playRoomCmd(app, func() tea.Msg { return answer })
		}
		cmd = app.approvalBoxView.Forward(msg)
	}
	t.Fatal("the approval window gave no answer")
	return nil
}

// TestAMembersCallIsDecidedInTheApprovalWindow pins a room's call in the chat's
// own approval window: the newest open call takes it with who asks and why and
// no Auto-Approve, its answer reaches the room, a refusal shows as an error and
// offers the call again, a call decided elsewhere leaves the window, the chat's
// own approval keeps it, and leaving the room takes the call off it.
func TestAMembersCallIsDecidedInTheApprovalWindow(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	enterTestRoom(t, app, rooms)
	require.Nil(t, app.stateManager.GetApprovalUIState(), "nothing waiting leaves the window closed")

	playRoomCmd(app, app.handleRoomEvents(tui.RoomPostEvent{Room: "release", Post: askedCall("p1", "call-1")}))
	state := app.stateManager.GetApprovalUIState()
	require.NotNil(t, state, "the member's call takes the approval window")
	require.Equal(t, "release", state.Room)
	require.Equal(t, "Bash", state.PendingToolCall.Function.Name)
	require.Equal(t, "tester in #release asks to run this.\nRejected by judge: tests were not asked for", state.Context)
	window := ansi.Strip(app.approvalBoxView.Render())
	require.Contains(t, window, "Approval required")
	require.NotContains(t, window, "Auto-Approve", "a room never switches its member's mode")

	answerInApprovalWindow(t, app, 0)
	require.Equal(t, 1, rooms.DecideCallCount())
	_, room, member, toolCallID, decision := rooms.DecideArgsForCall(0)
	require.Equal(t, []string{"release", "tester", "call-1"}, []string{room, member, toolCallID})
	require.Equal(t, roomsdomain.DecisionApprove, decision)
	require.Nil(t, app.stateManager.GetApprovalUIState(), "the answered call leaves the window")
	playRoomCmd(app, app.handleRoomEvents(tui.RoomPostEvent{Room: "release", Post: roomsdomain.Post{
		ID: "p1-decided", Author: roomsdomain.RoomAuthor, Kind: roomsdomain.PostKindDecision, Text: "You approved tester's Bash call.",
		Approval: &roomsdomain.Approval{ID: "call-1", Decision: roomsdomain.DecisionApprove}, Time: time.Now(),
	}}))
	require.Nil(t, app.stateManager.GetApprovalUIState(), "the room's decision keeps the window closed")

	playRoomCmd(app, app.handleRoomEvents(tui.RoomPostEvent{Room: "release", Post: askedCall("p2", "call-2")}))
	rooms.DecideReturns(errors.New("tester's call was already decided"))
	played := answerInApprovalWindow(t, app, 1)
	_, _, _, _, decision = rooms.DecideArgsForCall(1)
	require.Equal(t, roomsdomain.DecisionReject, decision)
	require.Contains(t, statusOf(t, played), "tester's call was already decided")
	require.Equal(t, "call-2", app.stateManager.GetApprovalUIState().PendingToolCall.ID, "a call the room did not take is offered again")

	decided := roomsdomain.Post{
		ID: "p3", Author: roomsdomain.RoomAuthor, Kind: roomsdomain.PostKindDecision, Text: "tester's Bash call was decided in its own chat.",
		Approval: &roomsdomain.Approval{ID: "call-2"}, Time: time.Now(),
	}
	playRoomCmd(app, app.handleRoomEvents(tui.RoomPostEvent{Room: "release", Post: decided}))
	require.Nil(t, app.stateManager.GetApprovalUIState(), "a call decided elsewhere leaves the window")

	own := &sdk.ChatCompletionMessageToolCall{ID: "own", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Write"}}
	app.stateManager.SetupApprovalUIState(own, make(chan agentdomain.ApprovalAction, 1))
	playRoomCmd(app, app.handleRoomEvents(tui.RoomPostEvent{Room: "release", Post: askedCall("p4", "call-4")}))
	require.Equal(t, "own", app.stateManager.GetApprovalUIState().PendingToolCall.ID, "the chat's own approval keeps the window")
	app.stateManager.ClearApprovalUIState()

	playRoomCmd(app, app.handleRoomEvents(tui.RoomPostEvent{Room: "release", Post: askedCall("p5", "call-5")}))
	require.Equal(t, "call-5", app.stateManager.GetApprovalUIState().PendingToolCall.ID)
	playRoomCmd(app, app.switchRoom(noRoom))
	require.Nil(t, app.stateManager.GetApprovalUIState(), "leaving the room takes its call off the window")
}

func TestOnlyTheRoomOnScreenTakesPostsAndActivity(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	enterTestRoom(t, app, rooms)
	app.roomView.SetWidth(80)
	app.roomView.SetHeight(20)

	app.handleRoomEvents(tui.RoomPostEvent{Room: "other", Post: roomsdomain.Post{Author: "tester", Text: "elsewhere", Time: time.Now()}})
	app.handleRoomEvents(tui.RoomPostEvent{Room: "release", Post: roomsdomain.Post{Author: "tester", Text: "all green", Usage: roomsdomain.Usage{Tokens: 2400}, Time: time.Now()}})
	app.handleRoomEvents(tui.RoomActivityEvent{Room: "release", Member: "writer", Activity: roomsdomain.ActivityRunning})

	screen := ansi.Strip(app.roomView.Render())
	require.NotContains(t, screen, "elsewhere")
	require.Contains(t, screen, "all green")
	require.Contains(t, screen, "writer is working…")
	require.Contains(t, screen, "2.4k tokens", "the header adds up what the room's posts cost")

	app.handleRoomEvents(tui.RoomClearedEvent{Room: "other"})
	require.Contains(t, ansi.Strip(app.roomView.Render()), "all green", "another room's clear leaves this one alone")
	played := playRoomCmd(app, app.handleRoomEvents(tui.RoomClearedEvent{Room: "release"}))
	require.NotContains(t, ansi.Strip(app.roomView.Render()), "all green")
	require.Contains(t, statusOf(t, played), "#release was cleared")
}
