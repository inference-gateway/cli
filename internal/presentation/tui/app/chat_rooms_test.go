package app

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"
	ansi "github.com/charmbracelet/x/ansi"

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

	approve, reject := roomApprovalBindings(cfg.Chat.Keybindings)
	app := &ChatApplication{
		config:                  cfg,
		inputStatusBar:          statusBar,
		inputView:               components.NewInputView(modelService),
		autocomplete:            ac,
		stateManager:            statemanager.NewStore(false),
		conversationView:        components.NewConversationView(styleProvider),
		applicationViewRenderer: components.NewApplicationViewRenderer(styleProvider),
		roomPicker:              components.NewPicker(styleProvider),
		roomView:                components.NewRoomView(styleProvider),
		newSession:              newSessionBinding(cfg.Chat.Keybindings),
		cycleMode:               cycleModeBinding(cfg.Chat.Keybindings),
		modeIndicator:           modeIndicatorOf(styleProvider),
		roomApprove:             approve,
		roomReject:              reject,
	}
	app.showRoomApprovalKeys()
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

// TestAMembersCallIsDecidedFromTheRoom pins the room's approval in the chat:
// the approve key and a click on Reject decide the newest open call, the keys
// go on when nothing waits, and the daemon's refusal shows as an error.
func TestAMembersCallIsDecidedFromTheRoom(t *testing.T) {
	app, rooms := newRoomTestApp(t)
	enterTestRoom(t, app, rooms)
	app.roomView.SetWidth(80)
	app.roomView.SetHeight(20)
	approveKey := tea.KeyPressMsg{Code: 'y', Mod: tea.ModAlt}
	require.Nil(t, app.decideByKey(approveKey), "with nothing waiting the key goes on to the composer")

	app.handleRoomEvents(tui.RoomPostEvent{Room: "release", Post: roomsdomain.Post{
		ID: "p1", Author: "tester", Kind: roomsdomain.PostKindApproval, Text: "Rejected by judge: tests were not asked for",
		Approval: &roomsdomain.Approval{ID: "call-1", Tool: "Bash", Arguments: `{"command":"go test ./..."}`}, Time: time.Now(),
	}})
	playRoomCmd(app, tea.Batch(app.handleChatViewKeyPress(approveKey)...))
	require.Equal(t, 1, rooms.DecideCallCount())
	_, room, member, toolCallID, decision := rooms.DecideArgsForCall(0)
	require.Equal(t, []string{"release", "tester", "call-1"}, []string{room, member, toolCallID})
	require.Equal(t, roomsdomain.DecisionApprove, decision)

	lines := strings.Split(ansi.Strip(app.roomView.Render()), "\n")
	row := slices.IndexFunc(lines, func(line string) bool { return strings.Contains(line, "[ Reject ]") })
	require.GreaterOrEqual(t, row, 0, "the open call shows its choices")
	rooms.DecideReturns(errors.New("tester's call was already decided"))
	click := tea.MouseClickMsg{X: strings.Index(lines[row], "[ Reject ]"), Y: row + app.applicationViewRenderer.ConversationTop(), Button: tea.MouseLeft}
	played := playRoomCmd(app, tea.Batch(app.handleChatView(click)...))
	require.Equal(t, 2, rooms.DecideCallCount())
	_, _, _, _, decision = rooms.DecideArgsForCall(1)
	require.Equal(t, roomsdomain.DecisionReject, decision)
	require.Contains(t, statusOf(t, played), "tester's call was already decided")
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
