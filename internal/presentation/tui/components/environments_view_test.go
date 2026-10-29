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
	onLaptop     = tui.SessionPlacement{Daemon: true, Local: "laptop"}
	onK8sDev     = tui.SessionPlacement{Daemon: true, Local: "laptop", Environment: "k8s-dev"}
	movingToDev  = tui.SessionPlacement{Daemon: true, Local: "laptop", Transferring: "k8s-dev"}
	environments = []agentdomain.EnvironmentStatus{
		{Name: "laptop", Kind: agentdomain.EnvironmentKindLocal, Reachable: true},
		{Name: "k8s-dev", Kind: agentdomain.EnvironmentKindKubernetes, Reachable: true, Capabilities: map[string]string{"namespace": "infer"}},
		{Name: "k8s-gone", Kind: agentdomain.EnvironmentKindKubernetes, Error: "connection refused"},
	}
)

// picker is an environments picker that counts its listings.
type picker struct {
	view  *EnvironmentsView
	loads int
}

// openPicker opens the picker for a session at placement with the
// environments listed, its cursor on row.
func openPicker(placement tui.SessionPlacement, row int) *picker {
	p := &picker{}
	p.view = NewEnvironmentsView(createMockStyleProvider(), func() tea.Cmd { p.loads++; return nil })
	p.view.Reset(placement)
	p.view.Update(tui.EnvironmentsLoadedEvent{Environments: environments})
	for range row {
		p.press(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	return p
}

func (p *picker) press(key tea.KeyPressMsg) tea.Cmd {
	_, cmd := p.view.Update(key)
	return cmd
}

func (p *picker) row(name string) environmentItem {
	for _, item := range p.view.list.Items() {
		if it := item.(environmentItem); it.status.Name == name {
			return it
		}
	}
	return environmentItem{}
}

func TestEnvironmentsViewMarksWhereTheSessionRuns(t *testing.T) {
	p := openPicker(onLaptop, 0)

	if !p.row("laptop").current || p.row("k8s-dev").current {
		t.Error("the picker does not mark laptop, and only laptop, as where the session runs")
	}
	if title := p.view.list.Title; title != "Environments (3) · session on laptop" {
		t.Errorf("title = %q", title)
	}

	p.view.Update(tui.SessionPlacementChangedEvent{Placement: movingToDev})
	if !p.row("k8s-dev").moving {
		t.Error("the picker does not mark the environment the session moves to")
	}
}

func TestEnvironmentsViewMovesTheSessionOnEnter(t *testing.T) {
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
			if request, ok := cmd().(tui.TransferRequestedEvent); !ok || request.Target != tt.target {
				t.Fatalf("enter asked for %+v, want a move to %s", cmd(), tt.target)
			}
			if !p.view.IsCancelled() {
				t.Error("the picker stayed open after the move started")
			}
		})
	}
}

func TestEnvironmentsViewSaysWhyItCannotMoveThere(t *testing.T) {
	tests := []struct {
		name      string
		placement tui.SessionPlacement
		row       int
		reason    string
	}{
		{name: "where it runs", placement: onLaptop, row: 0, reason: "already runs on laptop"},
		{name: "an unreachable environment", placement: onLaptop, row: 2, reason: "k8s-gone is unreachable"},
		{name: "on from a remote environment", placement: onK8sDev, row: 2, reason: "moves only back to laptop"},
		{name: "during a transfer", placement: movingToDev, row: 1, reason: "already running"},
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

func TestEnvironmentsViewProbesAgainOnR(t *testing.T) {
	p := &picker{}
	p.view = NewEnvironmentsView(createMockStyleProvider(), func() tea.Cmd { p.loads++; return nil })
	p.view.Reset(onLaptop)
	if !p.view.IsLoading() || !strings.Contains(p.view.list.Title, "probing") {
		t.Fatalf("an opened picker is not probing: %q", p.view.list.Title)
	}

	p.view.Update(tui.EnvironmentsLoadedEvent{Err: errors.New("daemon gone")})
	if p.view.IsLoading() || !strings.Contains(p.view.list.Title, "daemon gone") {
		t.Fatalf("title = %q, want the listing's error", p.view.list.Title)
	}

	p.press(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if p.loads != 2 || !p.view.IsLoading() {
		t.Errorf("listings = %d, loading = %v, want r to probe again", p.loads, p.view.IsLoading())
	}
}

func TestEnvironmentsViewClosesOnEsc(t *testing.T) {
	p := openPicker(onLaptop, 0)

	if cmd := p.press(tea.KeyPressMsg{Code: tea.KeyEscape}); cmd != nil || !p.view.IsCancelled() {
		t.Error("esc did not close the picker without moving the session")
	}
}
