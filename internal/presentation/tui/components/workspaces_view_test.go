package components

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

var (
	onLaptop         = tui.SessionPlacement{Daemon: true, Local: "laptop"}
	onK8sDev         = tui.SessionPlacement{Daemon: true, Local: "laptop", Workspace: "k8s-dev"}
	teleportingToDev = tui.SessionPlacement{Daemon: true, Local: "laptop", Teleporting: "k8s-dev"}
	workspaces       = []agentdomain.WorkspaceStatus{
		{Name: "laptop", Kind: agentdomain.WorkspaceKindLocal, Reachable: true},
		{Name: "k8s-dev", Kind: agentdomain.WorkspaceKindKubernetes, Reachable: true, Capabilities: map[string]string{"namespace": "infer"}},
		{Name: "k8s-gone", Kind: agentdomain.WorkspaceKindKubernetes, Error: "connection refused"},
	}
)

// picker is a workspaces picker that counts its listings.
type picker struct {
	view  *WorkspacesView
	loads int
}

// openPicker opens the picker for a session at placement with the
// workspaces listed, its cursor on row.
func openPicker(placement tui.SessionPlacement, row int) *picker {
	p := &picker{}
	p.view = NewWorkspacesView(createMockStyleProvider(), func() tea.Cmd { p.loads++; return nil })
	p.view.Reset(placement)
	p.view.Update(tui.WorkspacesLoadedEvent{Workspaces: workspaces})
	for range row {
		p.press(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	return p
}

func (p *picker) press(key tea.KeyPressMsg) tea.Cmd {
	_, cmd := p.view.Update(key)
	return cmd
}

func (p *picker) row(name string) workspaceItem {
	for _, item := range p.view.list.Items() {
		if it := item.(workspaceItem); it.status.Name == name {
			return it
		}
	}
	return workspaceItem{}
}

func TestWorkspacesViewMarksWhereTheSessionRuns(t *testing.T) {
	p := openPicker(onLaptop, 0)

	if !p.row("laptop").current || p.row("k8s-dev").current {
		t.Error("the picker does not mark laptop, and only laptop, as where the session runs")
	}
	if title := p.view.list.Title; title != "Workspaces (3) · session on laptop" {
		t.Errorf("title = %q", title)
	}

	p.view.Update(tui.SessionPlacementChangedEvent{Placement: teleportingToDev})
	if !p.row("k8s-dev").teleporting {
		t.Error("the picker does not mark the workspace the session teleports to")
	}
}

func TestWorkspacesViewTeleportsTheSessionOnEnter(t *testing.T) {
	tests := []struct {
		name      string
		placement tui.SessionPlacement
		row       int
		target    string
	}{
		{name: "from the laptop to k8s-dev", placement: onLaptop, row: 1, target: "k8s-dev"},
		{name: "from k8s-dev back to the laptop", placement: onK8sDev, row: 0, target: "laptop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := openPicker(tt.placement, tt.row)

			cmd := p.press(tea.KeyPressMsg{Code: tea.KeyEnter})

			if cmd == nil {
				t.Fatalf("enter refused: %s", p.view.list.Title)
			}
			if request, ok := cmd().(tui.TeleportRequestedEvent); !ok || request.Target != tt.target {
				t.Fatalf("enter asked for %+v, want a teleport to %s", cmd(), tt.target)
			}
			if !p.view.IsCancelled() {
				t.Error("the picker stayed open after the teleport started")
			}
		})
	}
}

func TestWorkspacesViewSaysWhyItCannotTeleportThere(t *testing.T) {
	tests := []struct {
		name      string
		placement tui.SessionPlacement
		row       int
		reason    string
	}{
		{name: "where it runs", placement: onLaptop, row: 0, reason: "already runs on laptop"},
		{name: "an unreachable workspace", placement: onLaptop, row: 2, reason: "k8s-gone is unreachable"},
		{name: "on from a remote workspace", placement: onK8sDev, row: 2, reason: "teleports only back to laptop"},
		{name: "during a teleport", placement: teleportingToDev, row: 1, reason: "already running"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := openPicker(tt.placement, tt.row)

			if cmd := p.press(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
				t.Fatalf("enter asked for %+v, want a refusal", cmd())
			}
			if title := p.view.list.Title; !strings.Contains(title, tt.reason) {
				t.Errorf("title = %q, want it to say %q", title, tt.reason)
			}
			if p.view.IsCancelled() {
				t.Error("a refusal closed the picker")
			}
		})
	}
}

func TestWorkspacesViewProbesAgainOnR(t *testing.T) {
	p := &picker{}
	p.view = NewWorkspacesView(createMockStyleProvider(), func() tea.Cmd { p.loads++; return nil })
	p.view.Reset(onLaptop)
	if !p.view.IsLoading() || !strings.Contains(p.view.list.Title, "probing") {
		t.Fatalf("an opened picker is not probing: %q", p.view.list.Title)
	}

	p.view.Update(tui.WorkspacesLoadedEvent{Err: errors.New("daemon gone")})
	if p.view.IsLoading() || !strings.Contains(p.view.list.Title, "daemon gone") {
		t.Fatalf("title = %q, want the listing's error", p.view.list.Title)
	}

	p.press(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if p.loads != 2 || !p.view.IsLoading() {
		t.Errorf("listings = %d, loading = %v, want r to probe again", p.loads, p.view.IsLoading())
	}
}

func TestWorkspacesViewClosesOnEsc(t *testing.T) {
	p := openPicker(onLaptop, 0)

	if cmd := p.press(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil || !p.view.IsCancelled() {
		t.Error("esc did not close the picker without teleporting the session")
	}
}
