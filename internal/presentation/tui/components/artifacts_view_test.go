package components

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

var sessionArtifacts = []tui.Artifact{
	{Name: "recording.gif", Size: 2048, ModifiedAt: time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)},
	{Name: "shot.png", Size: 512, ModifiedAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)},
}

// artifactsBrowser is an open artifacts view that counts its listings.
type artifactsBrowser struct {
	view  *ArtifactsView
	loads int
}

func openArtifacts(artifacts []tui.Artifact, row int) *artifactsBrowser {
	b := &artifactsBrowser{}
	b.view = NewArtifactsView(createMockStyleProvider(), func() tea.Cmd { b.loads++; return nil })
	b.view.Reset(onK8sDev)
	b.view.Update(tui.ArtifactsLoadedEvent{Artifacts: artifacts})
	for range row {
		b.press(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	return b
}

func (b *artifactsBrowser) press(key tea.KeyPressMsg) tea.Cmd {
	_, cmd := b.view.Update(key)
	return cmd
}

func TestArtifactsViewListsNewestFirstWithSizeAndTime(t *testing.T) {
	b := openArtifacts(sessionArtifacts, 0)
	view := b.view.View().Content

	if !strings.Contains(b.view.list.Title, "Artifacts (2) · session on k8s-dev") {
		t.Fatalf("title = %q", b.view.list.Title)
	}
	if strings.Index(view, "recording.gif") > strings.Index(view, "shot.png") || !strings.Contains(view, "2.048kB") || !strings.Contains(view, "2026-11-03") {
		t.Fatalf("view =\n%s", view)
	}
}

func TestArtifactsViewKeysSaveOpenReloadAndClose(t *testing.T) {
	tests := []struct {
		name   string
		key    tea.KeyPressMsg
		row    int
		chosen string
		open   bool
		loads  int
		closed bool
	}{
		{name: "enter saves", key: tea.KeyPressMsg{Code: tea.KeyEnter}, row: 1, chosen: "shot.png", loads: 1},
		{name: "o saves and opens", key: tea.KeyPressMsg{Code: 'o', Text: "o"}, chosen: "recording.gif", open: true, loads: 1},
		{name: "r reloads", key: tea.KeyPressMsg{Code: 'r', Text: "r"}, loads: 2},
		{name: "esc closes", key: tea.KeyPressMsg{Code: tea.KeyEscape}, closed: true, loads: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := openArtifacts(sessionArtifacts, tt.row)
			cmd := b.press(tt.key)

			if b.loads != tt.loads || b.view.IsCancelled() != tt.closed {
				t.Fatalf("loads = %d, closed = %v", b.loads, b.view.IsCancelled())
			}
			if tt.chosen == "" {
				return
			}
			chosen, ok := cmd().(tui.ArtifactChosenEvent)
			if !ok || chosen.Name != tt.chosen || chosen.Open != tt.open {
				t.Fatalf("chose %+v, want %s open=%v", chosen, tt.chosen, tt.open)
			}
			if !strings.Contains(b.view.View().Content, "Saving "+tt.chosen) {
				t.Fatalf("view = %q", b.view.View().Content)
			}
		})
	}
}

func TestArtifactsViewShowsWhereTheArtifactLandedOrFailed(t *testing.T) {
	b := openArtifacts(sessionArtifacts, 0)
	b.view.SetWidth(40)
	home, _ := os.UserHomeDir()
	long := filepath.Join(home, ".infer", "projects", "-Users-me-Repositories-very-long-project-name", "artifacts", "c1", "shot.png")

	b.view.Update(tui.ArtifactSavedEvent{Name: "shot.png", Path: long})
	view := b.view.View().Content
	if !strings.Contains(view, "✓ Saved ~/.infer/projects/") || !strings.Contains(view, "artifacts/c1/shot.png") {
		t.Fatalf("view =\n%s", view)
	}
	if strings.Contains(b.view.list.Title, "Saved") {
		t.Fatalf("the path must wrap under the list, not truncate in the title:\n%s", view)
	}
	b.view.Update(tui.ArtifactSavedEvent{Name: "shot.png", Err: errors.New("the workspace hung up")})
	if !strings.Contains(strings.Join(strings.Fields(b.view.View().Content), " "), "Could not save shot.png: the workspace hung up") {
		t.Fatalf("view = %q", b.view.View().Content)
	}
}

func TestArtifactsViewShowsAListingErrorAndAnEmptySession(t *testing.T) {
	b := openArtifacts(nil, 0)
	if !strings.Contains(b.view.list.Title, "No artifacts yet") {
		t.Fatalf("title = %q", b.view.list.Title)
	}
	b.view.Update(tui.ArtifactsLoadedEvent{Err: errors.New("k8s-dev cannot be reached")})
	if !strings.Contains(b.view.list.Title, "Could not list artifacts: k8s-dev cannot be reached · r retries") {
		t.Fatalf("title = %q", b.view.list.Title)
	}
}
