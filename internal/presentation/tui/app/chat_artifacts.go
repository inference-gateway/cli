package app

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"

	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
)

// artifactsListTimeout bounds listing the artifacts, which may resume the
// workspace that owns the session first.
const artifactsListTimeout = 2 * time.Minute

// artifactFetchTimeout bounds reading one artifact from its workspace.
const artifactFetchTimeout = 5 * time.Minute

const noArtifactBrowserHint = "This chat has no session artifacts to browse"

// SetArtifactBrowser gives the chat the browser behind /artifacts and the
// artifacts indicator.
func (app *ChatApplication) SetArtifactBrowser(browser tui.ArtifactBrowser) {
	app.artifactBrowser = browser
}

// handleArtifactsEvents drives the artifacts view: opening it, saving the
// artifact it chose and reporting where it landed.
func (app *ChatApplication) handleArtifactsEvents(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case tui.ShowArtifactsEvent:
		return app.openArtifacts()
	case tui.ArtifactChosenEvent:
		return app.fetchArtifact(m)
	case tui.ArtifactSavedEvent:
		return app.artifactSaved(m)
	}
	return nil
}

func (app *ChatApplication) openArtifacts() tea.Cmd {
	if app.artifactBrowser == nil {
		return statusCmd(noArtifactBrowserHint)
	}
	if err := app.stateManager.TransitionToView(tui.ViewStateArtifacts); err != nil {
		return errorCmd(fmt.Sprintf("Failed to open the artifacts: %v", err), false)
	}
	return tea.Batch(statusCmd(""), app.artifactsView.Reset(app.stateManager.GetSessionPlacement()))
}

// loadArtifacts lists the session's artifacts off the update loop.
func (app *ChatApplication) loadArtifacts() tea.Cmd {
	browser := app.artifactBrowser
	if browser == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), artifactsListTimeout)
		defer cancel()
		artifacts, err := browser.List(ctx)
		return tui.ArtifactsLoadedEvent{Artifacts: artifacts, Err: err}
	}
}

// fetchArtifact saves the chosen artifact here off the update loop.
func (app *ChatApplication) fetchArtifact(chosen tui.ArtifactChosenEvent) tea.Cmd {
	browser := app.artifactBrowser
	if browser == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), artifactFetchTimeout)
		defer cancel()
		path, err := browser.Fetch(ctx, chosen.Name)
		return tui.ArtifactSavedEvent{Name: chosen.Name, Path: path, Open: chosen.Open, Err: err}
	}
}

// artifactSaved opens the saved artifact with the system viewer when asked.
// The view shows the outcome in its title.
func (app *ChatApplication) artifactSaved(saved tui.ArtifactSavedEvent) tea.Cmd {
	if saved.Err != nil || !saved.Open {
		return nil
	}
	return func() tea.Msg {
		if err := openWithSystemViewer(saved.Path); err != nil {
			return tui.ShowErrorEvent{Error: fmt.Sprintf("Saved %s, but could not open it: %v", saved.Path, err)}
		}
		return nil
	}
}

// openWithSystemViewer hands the file to the desktop's default application.
func openWithSystemViewer(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func (app *ChatApplication) handleArtifactsView(msg tea.Msg) []tea.Cmd {
	model, cmd := app.artifactsView.Update(msg)
	app.artifactsView = model.(*components.ArtifactsView)
	cmds := []tea.Cmd{cmd}
	if app.artifactsView.IsCancelled() {
		if err := app.stateManager.TransitionToView(tui.ViewStateChat); err != nil {
			cmds = append(cmds, errorCmd(fmt.Sprintf("Failed to return to chat: %v", err), false))
		}
		app.focusedComponent = app.inputView
	}
	return cmds
}

func (app *ChatApplication) renderArtifacts() string {
	width, height := app.stateManager.GetDimensions()
	app.artifactsView.SetWidth(width)
	app.artifactsView.SetHeight(height)
	return app.artifactsView.View().Content
}
