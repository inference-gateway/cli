package app

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
)

// environmentsTimeout bounds listing the environments, which probes each one.
const environmentsTimeout = 30 * time.Second

// transferSteps numbers the stages of a transfer for the status progress bar.
var transferSteps = map[agentdomain.TransferStage]int{
	agentdomain.TransferProvisioning:  1,
	agentdomain.TransferCheckpointing: 2,
	agentdomain.TransferRestoring:     3,
	agentdomain.TransferReady:         4,
}

// SetSessionMover gives a daemon chat the mover behind /environments and
// /transfer, and records where its session runs. An in-process chat has none.
func (app *ChatApplication) SetSessionMover(mover tui.SessionMover) {
	app.sessionMover = mover
	if mover != nil {
		app.stateManager.SetSessionPlacement(mover.Placement())
	}
}

// handleSessionPlacementEvents drives the environments picker and the
// transfer of a daemon chat's session.
func (app *ChatApplication) handleSessionPlacementEvents(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tui.ShowEnvironmentsEvent:
		return app.openEnvironments()
	case tui.TransferRequestedEvent:
		return app.requestTransfer(m)
	case tui.TransferProgressEvent:
		return app.transferProgressed(m.Progress)
	case tui.SessionPlacementChangedEvent:
		return app.placementChanged(m)
	}
	return nil
}

func (app *ChatApplication) openEnvironments() tea.Cmd {
	if app.sessionMover == nil {
		return statusCmd(daemonOnlyHint)
	}
	if err := app.stateManager.TransitionToView(tui.ViewStateEnvironments); err != nil {
		return errorCmd(fmt.Sprintf("Failed to open the environments: %v", err), false)
	}
	return tea.Batch(statusCmd(""), app.environmentsView.Reset(app.stateManager.GetSessionPlacement()))
}

// loadEnvironments lists the environments off the update loop.
func (app *ChatApplication) loadEnvironments() tea.Cmd {
	mover := app.sessionMover
	if mover == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), environmentsTimeout)
		defer cancel()
		environments, err := mover.ListEnvironments(ctx)
		return tui.EnvironmentsLoadedEvent{Environments: environments, Err: err}
	}
}

func (app *ChatApplication) handleEnvironmentsView(msg tea.Msg) []tea.Cmd {
	model, cmd := app.environmentsView.Update(msg)
	app.environmentsView = model.(*components.EnvironmentsView)
	cmds := []tea.Cmd{cmd}
	if app.environmentsView.IsCancelled() {
		if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
			cmds = append(cmds, errorCmd(fmt.Sprintf("Failed to return to chat: %v", err), false))
		}
		app.focusedComponent = app.inputView
	}
	return cmds
}

func (app *ChatApplication) renderEnvironments() string {
	width, height := app.stateManager.GetDimensions()
	app.environmentsView.SetWidth(width)
	app.environmentsView.SetHeight(height)
	return app.environmentsView.View().Content
}

// requestTransfer moves the session to the target, or says why it cannot go.
func (app *ChatApplication) requestTransfer(request tui.TransferRequestedEvent) tea.Cmd {
	if refusal := app.transferRefusal(request.Target); refusal != "" {
		return errorCmd(refusal, false)
	}
	if err := app.sessionMover.Transfer(request.Target, request.Files); err != nil {
		return errorCmd(fmt.Sprintf("Could not move the session to %s: %v", request.Target, err), false)
	}
	app.stateManager.SetSessionPlacement(app.sessionMover.Placement())
	return transferStatusCmd(request.Target, "requesting the move", 0)
}

// transferRefusal is why the session cannot move to target now, or "".
func (app *ChatApplication) transferRefusal(target string) string {
	placement := app.stateManager.GetSessionPlacement()
	switch {
	case app.sessionMover == nil:
		return daemonOnlyHint
	case placement.Offline:
		return "infer daemon is unreachable, reconnecting · try again once it is back"
	case placement.Transferring != "":
		return "A transfer to " + placement.Transferring + " is already running · esc cancels it"
	case placement.Remote():
		return "The session runs on " + placement.Environment + " and moves on only from " + placement.Environment + "'s own daemon"
	case target == placement.Where():
		return "The session already runs on " + target
	case app.stateManager.IsAgentBusy():
		return "Wait for the turn to finish before moving the session · esc stops it"
	}
	return ""
}

// transferProgressed shows the stage the running transfer reached. A failure
// stays on screen, since the session is left where it was.
func (app *ChatApplication) transferProgressed(progress agentdomain.TransferProgress) tea.Cmd {
	placement := app.stateManager.GetSessionPlacement()
	target := placement.Transferring
	if progress.Stage == agentdomain.TransferFailed {
		return errorCmd(fmt.Sprintf("Transfer to %s failed: %s. The session stays on %s.", target, progress.Message, placement.Where()), true)
	}
	return transferStatusCmd(target, transferStage(placement, progress.Stage), transferSteps[progress.Stage])
}

// placementChanged records where the session runs now and says so when it
// moved, lost the daemon or got it back.
func (app *ChatApplication) placementChanged(event tui.SessionPlacementChangedEvent) tea.Cmd {
	before := app.stateManager.GetSessionPlacement()
	app.stateManager.SetSessionPlacement(event.Placement)
	switch {
	case event.Moved:
		message := "✓ Session now runs on " + event.Placement.Where()
		if event.CancelledInterrupts > 0 {
			message += fmt.Sprintf(" · %d open prompt(s) were cancelled by the move", event.CancelledInterrupts)
		}
		return statusCmd(message)
	case event.Placement.Offline && !before.Offline:
		return func() tea.Msg {
			return tui.SetStatusEvent{Message: "Lost infer daemon, reconnecting…", Spinner: true, StatusType: tui.StatusWorking}
		}
	case before.Offline && !event.Placement.Offline && !app.stateManager.IsAgentBusy():
		return statusCmd("Reconnected to infer daemon")
	}
	return nil
}

// cancelTransfer stops the running transfer, which leaves the session here.
func (app *ChatApplication) cancelTransfer() []tea.Cmd {
	if err := app.sessionMover.CancelTransfer(); err != nil {
		return []tea.Cmd{errorCmd(fmt.Sprintf("Could not cancel the transfer: %v", err), false)}
	}
	return []tea.Cmd{statusCmd("Cancelling the transfer…")}
}

// transferring reports whether the session is moving, which blocks input.
func (app *ChatApplication) transferring() bool {
	return app.stateManager.GetSessionPlacement().Transferring != ""
}

// transferStage says what a stage of the move does.
func transferStage(placement tui.SessionPlacement, stage agentdomain.TransferStage) string {
	target := placement.Transferring
	return map[agentdomain.TransferStage]string{
		agentdomain.TransferProvisioning:  "provisioning " + target,
		agentdomain.TransferCheckpointing: "checkpointing the conversation and workspace",
		agentdomain.TransferRestoring:     "restoring on " + target,
		agentdomain.TransferReady:         "ready, reattaching",
	}[stage]
}

const daemonOnlyHint = "Environments are for daemon chats · start one with infer chat --daemon"

func transferStatusCmd(target, stage string, step int) tea.Cmd {
	return func() tea.Msg {
		return tui.SetStatusEvent{
			Message:    fmt.Sprintf("Moving session to %s · %s · esc cancels", target, stage),
			Spinner:    true,
			StatusType: tui.StatusWorking,
			Progress:   &tui.StatusProgress{Current: step, Total: len(transferSteps)},
		}
	}
}

func statusCmd(message string) tea.Cmd {
	return func() tea.Msg {
		return tui.SetStatusEvent{Message: message, StatusType: tui.StatusDefault}
	}
}

func errorCmd(message string, sticky bool) tea.Cmd {
	return func() tea.Msg { return tui.ShowErrorEvent{Error: message, Sticky: sticky} }
}
