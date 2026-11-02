package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

// withArtifacts gives the chat app an artifacts view over browser.
func withArtifacts(app *ChatApplication, browser tui.ArtifactBrowser) {
	themeService := &tuimocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(&tuimocks.FakeTheme{})
	app.artifactsView = components.NewArtifactsView(styles.NewProvider(themeService), app.loadArtifacts)
	app.SetArtifactBrowser(browser)
}

func TestOpeningTheArtifactsViewListsTheSessionsArtifacts(t *testing.T) {
	app, state := newDaemonChatApp(sessionOnK8sDev, &tuimocks.FakeSessionTeleporter{})
	browser := &tuimocks.FakeArtifactBrowser{}
	browser.ListReturns([]tui.Artifact{{Name: "shot.png"}}, nil)
	withArtifacts(app, browser)

	cmd := app.openArtifacts()

	if state.GetCurrentView() != tui.ViewStateArtifacts {
		t.Fatalf("view = %v, want the artifacts", state.GetCurrentView())
	}
	loaded, ok := batched(t, cmd, func(msg tea.Msg) bool { _, ok := msg.(tui.ArtifactsLoadedEvent); return ok }).(tui.ArtifactsLoadedEvent)
	if !ok || len(loaded.Artifacts) != 1 || browser.ListCallCount() != 1 {
		t.Fatalf("loaded %+v after %d listings", loaded, browser.ListCallCount())
	}
}

func TestArtifactsViewWithoutABrowserSaysSo(t *testing.T) {
	app, state := newDaemonChatApp(tui.SessionPlacement{}, nil)
	withArtifacts(app, nil)

	text, _ := message(t, app.openArtifacts())

	if !strings.Contains(text, "no session artifacts") || state.GetCurrentView() != tui.ViewStateChat {
		t.Fatalf("status %q in view %v", text, state.GetCurrentView())
	}
}

func TestInProcessChatBrowsesItsOwnArtifactsDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "shot.png"), []byte("png"), 0o600)
	app, _ := newDaemonChatApp(tui.SessionPlacement{}, nil)
	withArtifacts(app, tui.LocalArtifactBrowser{Dir: func() string { return dir }})

	loaded := app.loadArtifacts()().(tui.ArtifactsLoadedEvent)
	if loaded.Err != nil || len(loaded.Artifacts) != 1 || loaded.Artifacts[0].Name != "shot.png" {
		t.Fatalf("loaded %+v", loaded)
	}
	saved := app.fetchArtifact(tui.ArtifactChosenEvent{Name: "shot.png"})().(tui.ArtifactSavedEvent)
	if saved.Err != nil || saved.Path != filepath.Join(dir, "shot.png") {
		t.Fatalf("saved %+v", saved)
	}
}

func TestArtifactSavedOpensOnlyWhenAskedAndSaved(t *testing.T) {
	app, _ := newDaemonChatApp(tui.SessionPlacement{}, nil)
	withArtifacts(app, &tuimocks.FakeArtifactBrowser{})

	if cmd := app.artifactSaved(tui.ArtifactSavedEvent{Path: "/x", Open: false}); cmd != nil {
		t.Fatal("a plain save opened the file")
	}
	if cmd := app.artifactSaved(tui.ArtifactSavedEvent{Open: true, Err: errors.New("hung up")}); cmd != nil {
		t.Fatal("a failed save opened the file")
	}
	if cmd := app.artifactSaved(tui.ArtifactSavedEvent{Path: "/x", Open: true}); cmd == nil {
		t.Fatal("a save with open did nothing")
	}
}

// batched runs cmd and returns the first message, nested batches included,
// that want accepts.
func batched(t *testing.T, cmd tea.Cmd, want func(tea.Msg) bool) tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, inner := range batch {
			if found := batched(t, inner, want); found != nil {
				return found
			}
		}
		return nil
	}
	if want(msg) {
		return msg
	}
	return nil
}
