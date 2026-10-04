package app

import (
	"context"
	"reflect"
	"time"

	key "charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// TranscriptStore reads a stored conversation without touching the live one,
// so the chat can show what a running sub-agent has written so far.
type TranscriptStore interface {
	LoadConversation(ctx context.Context, conversationID string) ([]convdomain.ConversationEntry, convdomain.ConversationMetadata, error)
}

// jobTranscriptRefresh is how often a viewed job's transcript is re-read.
const jobTranscriptRefresh = time.Second

// jobTranscriptLoadedMsg carries a viewed job's transcript. found is false
// once the job was reaped and there is nothing left to show.
type jobTranscriptLoadedMsg struct {
	jobID   string
	entries []convdomain.ConversationEntry
	found   bool
}

// jobTranscriptTickMsg re-reads the viewed job's transcript.
type jobTranscriptTickMsg struct{ jobID string }

// jobWrapUpMsg reports whether a job took the wrap-up request, with the reason
// when it could not.
type jobWrapUpMsg struct {
	jobID string
	err   error
}

// wrapUpRequestedNote confirms a delivered wrap-up request under the job list.
const wrapUpRequestedNote = "wrap-up requested"

// SetTranscriptStore wires the store sub-agent transcripts are read from.
func (app *ChatApplication) SetTranscriptStore(store TranscriptStore) {
	app.transcriptStore = store
}

// focusJobList moves keyboard focus to the job list under the composer. It
// reports false when the list has no row to select.
func (app *ChatApplication) focusJobList() bool {
	if app.subagentList == nil || !app.subagentList.Focus() {
		return false
	}
	if app.statusBarFocused {
		app.blurStatusBar()
	}
	app.jobListFocused = true
	return true
}

// blurJobList returns focus to the input and the chat to its own transcript.
func (app *ChatApplication) blurJobList() {
	app.stopViewingJob()
	app.jobListFocused = false
	if app.subagentList != nil {
		app.subagentList.Blur()
	}
}

// handleFocusedRowKeys routes a key to whichever row below the input holds
// focus: the status indicators or the job list.
func (app *ChatApplication) handleFocusedRowKeys(keyMsg tea.KeyPressMsg) ([]tea.Cmd, bool) {
	switch {
	case key.Matches(keyMsg, guardKeys.interrupt):
		return nil, false
	case app.statusBarFocused:
		return app.handleStatusBarKeys(keyMsg)
	case app.jobListFocused:
		return app.handleJobListKeys(keyMsg)
	}
	return nil, false
}

// handleJobListKeys interprets keys while the job list holds focus. Typed text
// blurs the list and flows on to the input. Other unhandled keys flow on with
// focus kept, so the transcript can be scrolled while a job is viewed.
func (app *ChatApplication) handleJobListKeys(keyMsg tea.KeyPressMsg) ([]tea.Cmd, bool) {
	gk := guardKeys
	switch {
	case key.Matches(keyMsg, gk.jobPrev):
		if app.subagentList.SelectPrev() {
			return app.followSelection(), true
		}
		app.blurJobList()
		if app.inputStatusBar != nil && app.inputStatusBar.Focus() {
			app.statusBarFocused = true
		}
		return nil, true
	case key.Matches(keyMsg, gk.jobNext):
		app.subagentList.SelectNext()
		return app.followSelection(), true
	case key.Matches(keyMsg, gk.confirm):
		return app.viewSelectedJob(), true
	case key.Matches(keyMsg, gk.cancel):
		if app.viewedJobID == "" {
			app.blurJobList()
			return nil, true
		}
		app.stopViewingJob()
		return nil, true
	case key.Matches(keyMsg, gk.jobWrapUp):
		return app.wrapUpSelectedJob(), true
	case keyMsg.Text != "":
		app.blurJobList()
		return nil, false
	default:
		return nil, false
	}
}

// wrapUpSelectedJob asks the selected sub-agent or A2A task to finish and
// report. It runs off the update loop because an A2A task is reached over the
// network. Any other row ignores the key.
func (app *ChatApplication) wrapUpSelectedJob() []tea.Cmd {
	job, ok := app.subagentList.SelectedJob()
	if !ok || !app.subagentList.CanWrapUp() || app.backgroundTaskRegistry == nil {
		return nil
	}
	registry, jobID := app.backgroundTaskRegistry, job.Meta.ID
	return []tea.Cmd{func() tea.Msg {
		return jobWrapUpMsg{jobID: jobID, err: registry.WindJob(jobID, scheddomain.WindWrapUp)}
	}}
}

// followSelection keeps the transcript on the selected job while one is viewed.
func (app *ChatApplication) followSelection() []tea.Cmd {
	if app.viewedJobID == "" {
		return nil
	}
	return app.viewSelectedJob()
}

// viewSelectedJob swaps the chat transcript for the selected job's.
func (app *ChatApplication) viewSelectedJob() []tea.Cmd {
	job, ok := app.subagentList.SelectedJob()
	if !ok {
		return nil
	}
	app.viewedJobID = job.Meta.ID
	app.subagentList.SetViewing(job.Meta.ID)
	return []tea.Cmd{app.loadJobTranscript(job.Meta.ID)}
}

// stopViewingJob puts the chat's own transcript back on screen.
func (app *ChatApplication) stopViewingJob() {
	if app.viewedJobID == "" {
		return
	}
	app.viewedJobID = ""
	app.viewedTranscript = nil
	if app.subagentList != nil {
		app.subagentList.SetViewing("")
	}
	if app.conversationView != nil && app.conversationRepo != nil {
		app.conversationView.SetConversation(app.conversationRepo.GetMessages())
	}
}

// loadJobTranscript reads a job's transcript off the update loop.
func (app *ChatApplication) loadJobTranscript(jobID string) tea.Cmd {
	return func() tea.Msg {
		job, found := app.subagentList.Job(jobID)
		if !found {
			return jobTranscriptLoadedMsg{jobID: jobID}
		}
		return jobTranscriptLoadedMsg{jobID: jobID, entries: app.jobTranscript(job), found: true}
	}
}

// jobTranscript is what a job has to show: a sub-agent's stored conversation,
// otherwise the job's own output as a single message.
func (app *ChatApplication) jobTranscript(job scheddomain.TrackedJob) []convdomain.ConversationEntry {
	if entries := app.storedTranscript(job.Meta.SessionID); len(entries) > 0 {
		return entries
	}
	text := job.Output
	if text == "" {
		text = "No output yet."
	}
	return []convdomain.ConversationEntry{{
		Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent(text)},
		Time:    job.Meta.StartedAt,
	}}
}

// storedTranscript loads the conversation a job keeps under its session. It is
// nil for a job without one, with storage disabled or when the load fails.
func (app *ChatApplication) storedTranscript(sessionID string) []convdomain.ConversationEntry {
	if sessionID == "" || app.transcriptStore == nil {
		return nil
	}
	entries, _, err := app.transcriptStore.LoadConversation(context.Background(), sessionID)
	if err != nil {
		return nil
	}
	return entries
}

// handleJobTranscriptMsg shows a loaded transcript and keeps it fresh while
// its job stays on screen, and notes the outcome of a wrap-up request. It
// reports false for any other message.
func (app *ChatApplication) handleJobTranscriptMsg(msg tea.Msg) ([]tea.Cmd, bool) {
	switch msg := msg.(type) {
	case jobWrapUpMsg:
		note := wrapUpRequestedNote
		if msg.err != nil {
			note = msg.err.Error()
		}
		app.subagentList.SetNote(msg.jobID, note)
		return nil, true
	case jobTranscriptTickMsg:
		if msg.jobID != app.viewedJobID {
			return nil, true
		}
		return []tea.Cmd{app.loadJobTranscript(msg.jobID)}, true
	case jobTranscriptLoadedMsg:
		if msg.jobID != app.viewedJobID {
			return nil, true
		}
		if !msg.found {
			app.blurJobList()
			return nil, true
		}
		app.showJobTranscript(msg.entries)
		jobID := msg.jobID
		return []tea.Cmd{tea.Tick(jobTranscriptRefresh, func(time.Time) tea.Msg { return jobTranscriptTickMsg{jobID: jobID} })}, true
	}
	return nil, false
}

// showJobTranscript puts entries on screen, skipping an unchanged transcript so
// a refresh does not reset the scroll position.
func (app *ChatApplication) showJobTranscript(entries []convdomain.ConversationEntry) {
	if app.viewedTranscript != nil && reflect.DeepEqual(entries, app.viewedTranscript) {
		return
	}
	app.viewedTranscript = entries
	if app.conversationView != nil {
		app.conversationView.SetConversation(entries)
	}
}

// keepViewedTranscript swaps the history carried by a chat update for the
// viewed job's, so the chat's own updates cannot replace it on screen.
func (app *ChatApplication) keepViewedTranscript(msg tea.Msg) tea.Msg {
	if app.viewedJobID == "" || app.viewedTranscript == nil {
		return msg
	}
	switch msg := msg.(type) {
	case tui.UpdateHistoryEvent:
		msg.History = app.viewedTranscript
		return msg
	case tui.BashCommandCompletedEvent:
		msg.History = app.viewedTranscript
		return msg
	}
	return msg
}
