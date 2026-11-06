//go:build e2e

package e2e

import (
	"path/filepath"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"
)

func TestChatTUIAnswersAQuestion(t *testing.T) {
	const session = "infer-e2e-question"
	startLocalChat(t, session)

	chatSay(t, session, "question form probe")
	expectPane(t, session, "Which color fits best")
	tmuxSendKeys(t, session, "Enter")
	expectPane(t, session, "-> Red")
	expectPane(t, session, "QUESTION-FORM-DONE")
}

func TestChatTUICancelsAReplyAndKeepsGoing(t *testing.T) {
	const session = "infer-e2e-cancel"
	startLocalChat(t, session)

	chatSay(t, session, "cancel me probe")
	expectPane(t, session, "Starting response")
	tmuxSendKeys(t, session, "Escape")
	expectPane(t, session, "User interrupted")
	chatSay(t, session, "after the cancel probe")
	expectPane(t, session, "AFTER-CANCEL-OK")
}

// expectStatusSurvivesPanel opens a panel during a run and closes it again, and
// expects the run's status back on screen with the shortcut's menu gone.
func expectStatusSurvivesPanel(t *testing.T, session, shortcut, title string, daemons ...*daemonProc) {
	t.Helper()
	chatSay(t, session, "cancel me probe")
	expectPane(t, session, "Generating response", daemons...)
	chatSay(t, session, shortcut)
	expectPane(t, session, title, daemons...)
	tmuxSendKeys(t, session, "Escape")
	expectPane(t, session, "Generating response", daemons...)
	require.NotContains(t, capturePane(session), "Tab to select")
}

func TestChatTUIKeepsARunsStatusAfterClosingAPanel(t *testing.T) {
	tests := []struct {
		name     string
		session  string
		shortcut string
		title    string
	}{
		{"a panel shortcut", "infer-e2e-panel-status-tasks", "/tasks", "Background Tasks"},
		{"a shortcut that takes arguments", "infer-e2e-panel-status-theme", "/theme", "Select a Theme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startLocalChat(t, tt.session)

			expectStatusSurvivesPanel(t, tt.session, tt.shortcut, tt.title)
		})
	}
}

func TestChatTUIRunsAQueuedMessage(t *testing.T) {
	const session = "infer-e2e-queue"
	startLocalChat(t, session)

	chatSay(t, session, "queue first probe")
	expectPane(t, session, "Starting response")
	chatSay(t, session, "queue second probe")
	expectPane(t, session, "QUEUE-FIRST-REPLY")
	expectPane(t, session, "QUEUE-SECOND-REPLY")
}

func TestChatTUIShowsTodos(t *testing.T) {
	const session = "infer-e2e-todos"
	startLocalChat(t, session)

	chatSay(t, session, "todo list probe")
	expectPane(t, session, "TODO-LIST-DONE")
	expectPane(t, session, "second probe item")
}

func TestChatTUIReportsAProviderError(t *testing.T) {
	const session = "infer-e2e-provider-error"
	startLocalChat(t, session, "INFER_CLIENT_RETRY_ENABLED=false")

	chatSay(t, session, "this always fails")
	expectPane(t, session, "Chat error")
}

func TestChatTUIAutoApprovesTheRestOfABatch(t *testing.T) {
	const session = "infer-e2e-auto-approve"
	dir := startLocalChat(t, session)

	chatSay(t, session, "auto approve probe")
	expectPane(t, session, "Approval required")
	tmuxSendKeys(t, session, "Right")
	expectPane(t, session, "←  Reject  →")
	tmuxSendKeys(t, session, "Right")
	expectPane(t, session, "←  Auto-Approve  →")
	tmuxSendKeys(t, session, "Enter")
	expectPane(t, session, "AUTO-APPROVE-DONE")
	require.FileExists(t, filepath.Join(dir, "auto-a.txt"))
	require.FileExists(t, filepath.Join(dir, "auto-b.txt"))
}

// askForAPlan turns plan mode on and sends the prompt whose reply asks to
// approve a plan, leaving the plan prompt open.
func askForAPlan(t *testing.T, session string) {
	t.Helper()
	tmuxSendKeys(t, session, "BTab")
	expectPane(t, session, "▶ PLAN")
	chatSay(t, session, "plan accepted probe")
	expectPane(t, session, "Plan approval required")
}

func TestChatTUIRejectedPlanDoesNotRun(t *testing.T) {
	const session = "infer-e2e-plan-rejected"
	startLocalChat(t, session)

	askForAPlan(t, session)
	tmuxSendKeys(t, session, "Right", "Enter")
	expectPane(t, session, "Plan rejected")
	require.NotContains(t, capturePane(session), "PLAN-EXECUTED")
}

func TestChatTUIPlanApprovedStepByStepRuns(t *testing.T) {
	const session = "infer-e2e-plan-standard"
	startLocalChat(t, session)

	askForAPlan(t, session)
	tmuxSendKeys(t, session, "Right", "Right", "Enter")
	expectPane(t, session, "PLAN-EXECUTED")
}

// TestChatTUIAsksForSandboxAccess covers a prompt a tool opens while it runs,
// for a call no model step named.
func TestChatTUIAsksForSandboxAccess(t *testing.T) {
	const session = "infer-e2e-sandbox-access"
	startLocalChat(t, session)

	chatSay(t, session, "sandbox access probe")
	expectPane(t, session, "SandboxAccess")
	expectPane(t, session, "Tool approval required")
	tmuxSendKeys(t, session, "Enter")
	expectPane(t, session, "SANDBOX-ACCESS-DONE")
}

// TestChatTUIJudgeRejectionKeepsTheScreenIntact pins that a rejection stays off
// stderr, which would print over the screen while the TUI owns the terminal.
func TestChatTUIJudgeRejectionKeepsTheScreenIntact(t *testing.T) {
	const session = "infer-e2e-judge-reject"
	startLocalChat(t, session)

	tmuxSendKeys(t, session, "BTab", "BTab", "BTab")
	expectPane(t, session, "AUTO+JUDGE")
	chatSay(t, session, "run the forbidden command")
	expectPane(t, session, "the command was not permitted")
	require.NotContains(t, capturePane(session), "Action rejected by judge policy")
}

// TestChatTUITimeBudgetEndsARunWithAPromptOpen pins that a run the agent ended
// does not wait on the prompt it left open.
func TestChatTUITimeBudgetEndsARunWithAPromptOpen(t *testing.T) {
	const session = "infer-e2e-budget-prompt"
	startLocalChat(t, session, "INFER_AGENT_MAX_DURATION_MINUTES=1")

	chatSay(t, session, "approval tail probe")
	expectPane(t, session, "Approval required")
	tmuxSendKeys(t, session, "Enter")
	expectPane(t, session, "1 │ two")
	require.True(t, waitForPane(t, session, "Response complete", 90*time.Second),
		"the run never ended behind the open prompt; last frame:\n%s", capturePane(session))
	require.NotContains(t, capturePane(session), "Chat error")
}
