package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
)

// environmentsTimeout bounds listing the environments, which probes each one.
const environmentsTimeout = 30 * time.Second

// teleportSteps numbers the stages of a teleport for the status progress bar.
var teleportSteps = map[agentdomain.TeleportStage]int{
	agentdomain.TeleportProvisioning:  1,
	agentdomain.TeleportCheckpointing: 2,
	agentdomain.TeleportRestoring:     3,
	agentdomain.TeleportReady:         4,
}

// placedFiles offers the project's files to the input while the session can
// read this checkout, and none while it runs on another environment.
type placedFiles struct {
	agentdomain.FileService
	placement func() tui.SessionPlacement
}

// localFiles wraps files so the input offers them only where the session runs.
func localFiles(files agentdomain.FileService, placement func() tui.SessionPlacement) agentdomain.FileService {
	if files == nil {
		return nil
	}
	return placedFiles{FileService: files, placement: placement}
}

func (f placedFiles) ListProjectFiles() ([]string, error) {
	if f.placement().Unavailable(tui.FeatureLocalFiles) != "" {
		return nil, nil
	}
	return f.FileService.ListProjectFiles()
}

func (f placedFiles) ValidateFile(path string) error {
	if reason := f.placement().Unavailable(tui.FeatureLocalFiles); reason != "" {
		return errors.New(reason)
	}
	return f.FileService.ValidateFile(path)
}

// SetSessionTeleporter gives a daemon chat the teleporter behind /environments and
// /teleport, and records where its session runs. An in-process chat has none.
func (app *ChatApplication) SetSessionTeleporter(teleporter tui.SessionTeleporter) {
	app.sessionTeleporter = teleporter
	if teleporter != nil {
		app.stateManager.SetSessionPlacement(teleporter.Placement())
	}
}

// handleSessionPlacementEvents drives the environments picker and the
// teleport of a daemon chat's session.
func (app *ChatApplication) handleSessionPlacementEvents(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tui.ShowEnvironmentsEvent:
		return app.openEnvironments()
	case tui.TeleportRequestedEvent:
		return app.requestTeleport(m)
	case tui.TeleportProgressEvent:
		return app.teleportProgressed(m.Progress)
	case tui.SessionPlacementChangedEvent:
		return app.placementChanged(m)
	}
	return nil
}

func (app *ChatApplication) openEnvironments() tea.Cmd {
	if app.sessionTeleporter == nil {
		return statusCmd(daemonOnlyHint)
	}
	if err := app.stateManager.TransitionToView(tui.ViewStateEnvironments); err != nil {
		return errorCmd(fmt.Sprintf("Failed to open the environments: %v", err), false)
	}
	return tea.Batch(statusCmd(""), app.environmentsView.Reset(app.stateManager.GetSessionPlacement()))
}

// loadEnvironments lists the environments off the update loop.
func (app *ChatApplication) loadEnvironments() tea.Cmd {
	teleporter := app.sessionTeleporter
	if teleporter == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), environmentsTimeout)
		defer cancel()
		environments, err := teleporter.ListEnvironments(ctx)
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

// requestTeleport teleports the session to the target, or says why it cannot go.
func (app *ChatApplication) requestTeleport(request tui.TeleportRequestedEvent) tea.Cmd {
	if refusal := app.teleportRefusal(request.Target); refusal != "" {
		return errorCmd(refusal, false)
	}
	if err := app.sessionTeleporter.Teleport(request.Target, request.Files); err != nil {
		return errorCmd(fmt.Sprintf("Could not teleport the session to %s: %v", request.Target, err), false)
	}
	app.stateManager.SetSessionPlacement(app.sessionTeleporter.Placement())
	return teleportStatusCmd(request.Target, "requesting the teleport", 0)
}

// teleportRefusal is why the session cannot teleport to target now, or "".
func (app *ChatApplication) teleportRefusal(target string) string {
	placement := app.stateManager.GetSessionPlacement()
	switch {
	case app.sessionTeleporter == nil:
		return daemonOnlyHint
	case placement.Offline:
		return "infer daemon is unreachable, reconnecting · try again once it is back"
	case placement.Teleporting != "":
		return "A teleport to " + placement.Teleporting + " is already running · esc cancels it"
	case placement.Remote() && target != placement.Local:
		return "From " + placement.Environment + " the session teleports only back to " + placement.Local
	case target == placement.Where():
		return "The session already runs on " + target
	case app.stateManager.IsAgentBusy():
		return "Wait for the turn to finish before teleporting the session · esc stops it"
	}
	return ""
}

// teleportProgressed shows the stage the running teleport reached. A failure
// stays on screen, since the session is left where it was.
func (app *ChatApplication) teleportProgressed(progress agentdomain.TeleportProgress) tea.Cmd {
	placement := app.stateManager.GetSessionPlacement()
	target := placement.Teleporting
	if progress.Stage == agentdomain.TeleportFailed {
		return errorCmd(fmt.Sprintf("Teleport to %s failed: %s. The session stays on %s.", target, progress.Message, placement.Where()), true)
	}
	return teleportStatusCmd(target, teleportStage(placement, progress.Stage), teleportSteps[progress.Stage])
}

// placementChanged records where the session runs now and says so when it
// teleported, lost the daemon or got it back.
func (app *ChatApplication) placementChanged(event tui.SessionPlacementChangedEvent) tea.Cmd {
	before := app.stateManager.GetSessionPlacement()
	app.stateManager.SetSessionPlacement(event.Placement)
	switch {
	case event.Teleported:
		message := "✓ Session now runs on " + event.Placement.Where()
		if event.CancelledInterrupts > 0 {
			message += fmt.Sprintf(" · %d open prompt(s) were cancelled by the teleport", event.CancelledInterrupts)
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

// cancelTeleport stops the running teleport, which leaves the session here.
func (app *ChatApplication) cancelTeleport() []tea.Cmd {
	if err := app.sessionTeleporter.CancelTeleport(); err != nil {
		return []tea.Cmd{errorCmd(fmt.Sprintf("Could not cancel the teleport: %v", err), false)}
	}
	return []tea.Cmd{statusCmd("Cancelling the teleport…")}
}

// teleporting reports whether the session is teleporting, which blocks input.
func (app *ChatApplication) teleporting() bool {
	return app.stateManager.GetSessionPlacement().Teleporting != ""
}

// teleportStage says what a stage of the teleport does, which differs on the way
// back from where the session runs.
func teleportStage(placement tui.SessionPlacement, stage agentdomain.TeleportStage) string {
	target := placement.Teleporting
	if placement.Remote() && target == placement.Local {
		from := placement.Environment
		return map[agentdomain.TeleportStage]string{
			agentdomain.TeleportProvisioning:  "reaching " + from,
			agentdomain.TeleportCheckpointing: "packing the session on " + from,
			agentdomain.TeleportRestoring:     "landing its workspace and conversation here",
			agentdomain.TeleportReady:         "ready, reattaching",
		}[stage]
	}
	return map[agentdomain.TeleportStage]string{
		agentdomain.TeleportProvisioning:  "provisioning " + target,
		agentdomain.TeleportCheckpointing: "checkpointing the conversation and workspace",
		agentdomain.TeleportRestoring:     "restoring on " + target,
		agentdomain.TeleportReady:         "ready, reattaching",
	}[stage]
}

const daemonOnlyHint = "Environments are for daemon chats · start one with infer chat --daemon"

func teleportStatusCmd(target, stage string, step int) tea.Cmd {
	return func() tea.Msg {
		return tui.SetStatusEvent{
			Message:    fmt.Sprintf("Teleporting session to %s · %s · esc cancels", target, stage),
			Spinner:    true,
			StatusType: tui.StatusWorking,
			Progress:   &tui.StatusProgress{Current: step, Total: len(teleportSteps)},
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
