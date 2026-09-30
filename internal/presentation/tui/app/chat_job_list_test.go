package app

import (
	"context"
	"testing"
	"time"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

type stubTranscriptStore map[string][]convdomain.ConversationEntry

func (s stubTranscriptStore) LoadConversation(_ context.Context, id string) ([]convdomain.ConversationEntry, convdomain.ConversationMetadata, error) {
	return s[id], convdomain.ConversationMetadata{}, nil
}

func textEntry(text string) convdomain.ConversationEntry {
	return convdomain.ConversationEntry{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent(text)}}
}

func entryText(t *testing.T, entries []convdomain.ConversationEntry) string {
	t.Helper()
	if len(entries) == 0 {
		return ""
	}
	text, err := entries[len(entries)-1].Message.Content.AsMessageContent0()
	if err != nil {
		t.Fatalf("entry has no text content: %v", err)
	}
	return text
}

// newJobListTestApp wires a chat with its own one-message transcript, a
// running headless sub-agent with a stored conversation and a running shell.
func newJobListTestApp(t *testing.T) (*ChatApplication, *tuimocks.FakeConversationRenderer) {
	t.Helper()
	app, _ := newStatusBarTestApp(t, false, false)

	now := time.Now()
	registry := &schedmocks.FakeBackgroundTaskRegistry{}
	registry.SnapshotReturns([]scheddomain.TrackedJob{
		{Meta: scheddomain.JobMeta{ID: "sub-1", Kind: scheddomain.JobKindSubagent, Label: "reviewer", StartedAt: now.Add(-time.Second)}, Status: scheddomain.JobRunning},
		{Meta: scheddomain.JobMeta{ID: "shell-1", Kind: scheddomain.JobKindShell, Detail: "npm run build", StartedAt: now.Add(-time.Minute)}, Status: scheddomain.JobRunning, Output: "building"},
	})
	registry.GetSubagentStub = func(id string) *scheddomain.SubagentState {
		if id != "sub-1" {
			return nil
		}
		return &scheddomain.SubagentState{ID: id, SessionID: "subagent-session", Mode: scheddomain.SubagentModeHeadless}
	}

	list := components.NewSubagentList(styles.NewProvider(styles.NewThemeProvider()))
	list.SetRegistry(registry)

	repo := &convmocks.FakeConversationRepository{}
	repo.GetMessagesReturns([]convdomain.ConversationEntry{textEntry("the chat's own answer")})

	view := &tuimocks.FakeConversationRenderer{}
	app.subagentList = list
	app.backgroundTaskRegistry = registry
	app.conversationRepo = repo
	app.conversationView = view
	app.SetTranscriptStore(stubTranscriptStore{"subagent-session": {textEntry("reading files"), textEntry("running tests")}})
	return app, view
}

func runCmds(t *testing.T, app *ChatApplication, cmds []tea.Cmd) []tea.Cmd {
	t.Helper()
	if len(cmds) != 1 {
		t.Fatalf("expected one command, got %d", len(cmds))
	}
	next, handled := app.handleJobTranscriptMsg(cmds[0]())
	if !handled {
		t.Fatal("expected the command's message to be a job transcript message")
	}
	return next
}

func shown(t *testing.T, view *tuimocks.FakeConversationRenderer) string {
	t.Helper()
	if view.SetConversationCallCount() == 0 {
		return ""
	}
	return entryText(t, view.SetConversationArgsForCall(view.SetConversationCallCount()-1))
}

func TestJobListFocusAndTranscriptSwap(t *testing.T) {
	app, view := newJobListTestApp(t)

	app.handleChatView(tui.FocusStatusBarEvent{})
	_ = app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyDown})
	if !app.jobListFocused || app.statusBarFocused {
		t.Fatalf("down on the status row should move focus to the job list, got list=%v status=%v", app.jobListFocused, app.statusBarFocused)
	}

	tick := runCmds(t, app, app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyEnter}))
	if got := shown(t, view); got != "running tests" {
		t.Fatalf("enter should show the sub-agent's stored conversation, got %q", got)
	}
	if len(tick) != 1 {
		t.Fatalf("a shown transcript should arm one refresh, got %d commands", len(tick))
	}

	masked, ok := app.keepViewedTranscript(tui.UpdateHistoryEvent{History: []convdomain.ConversationEntry{textEntry("chat update")}}).(tui.UpdateHistoryEvent)
	if !ok || entryText(t, masked.History) != "running tests" {
		t.Fatalf("a chat update must not replace the viewed transcript, got %+v", masked)
	}

	runCmds(t, app, app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyDown}))
	if got := shown(t, view); got != "building" {
		t.Fatalf("down while viewing should switch to the shell's output, got %q", got)
	}

	_ = app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := shown(t, view); got != "the chat's own answer" {
		t.Fatalf("esc should restore the chat's own transcript, got %q", got)
	}
	if !app.jobListFocused || app.viewedJobID != "" {
		t.Fatalf("the first esc only stops viewing, got focused=%v viewing=%q", app.jobListFocused, app.viewedJobID)
	}
	_ = app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape})
	if app.jobListFocused {
		t.Fatal("the second esc should return focus to the input")
	}
}

func TestJobListKeysWhileFocused(t *testing.T) {
	app, view := newJobListTestApp(t)
	if !app.focusJobList() {
		t.Fatal("expected the list to take focus while jobs run")
	}
	runCmds(t, app, app.viewSelectedJob())

	if _, handled := app.handleJobListKeys(tea.KeyPressMsg{Code: tea.KeyPgUp}); handled || !app.jobListFocused {
		t.Fatal("a scroll key should flow on and keep the list focused")
	}
	if _, handled := app.handleJobListKeys(tea.KeyPressMsg{Text: "a", Code: 'a'}); handled || app.jobListFocused {
		t.Fatal("typing should flow on to the input and blur the list")
	}
	if got := shown(t, view); got != "the chat's own answer" {
		t.Fatalf("typing should restore the chat's own transcript, got %q", got)
	}

	app.focusJobList()
	if _, handled := app.handleJobListKeys(tea.KeyPressMsg{Code: tea.KeyUp}); !handled || app.jobListFocused || !app.statusBarFocused {
		t.Fatal("up on the first row should return focus to the status row")
	}
}

func TestJobListFocusNeedsARow(t *testing.T) {
	app, _ := newJobListTestApp(t)
	app.backgroundTaskRegistry.(*schedmocks.FakeBackgroundTaskRegistry).SnapshotReturns(nil)
	if app.focusJobList() {
		t.Fatal("an empty list must not take focus")
	}

	app, _ = newJobListTestApp(t)
	app.focusJobList()
	runCmds(t, app, app.viewSelectedJob())
	app.backgroundTaskRegistry.(*schedmocks.FakeBackgroundTaskRegistry).SnapshotReturns(nil)
	if _, handled := app.handleJobTranscriptMsg(app.loadJobTranscript(app.viewedJobID)()); !handled || app.jobListFocused {
		t.Fatal("a reaped job should end the view and return focus to the input")
	}
}
