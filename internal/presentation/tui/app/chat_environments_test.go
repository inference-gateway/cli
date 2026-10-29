package app

import (
	"slices"
	"strings"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	components "github.com/inference-gateway/cli/internal/presentation/tui/components"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
)

var (
	sessionOnLaptop = tui.SessionPlacement{Daemon: true, Local: "laptop"}
	sessionOnK8sDev = tui.SessionPlacement{Daemon: true, Local: "laptop", Environment: "k8s-dev"}
)

// newDaemonChatApp is the chat surface the environments flow drives, for a
// session at placement. A nil mover is an in-process chat.
func newDaemonChatApp(placement tui.SessionPlacement, mover tui.SessionMover) (*ChatApplication, *statemanager.Store) {
	state := statemanager.NewStore(false)
	_ = state.TransitionToView(tui.ViewStateChat)
	app := &ChatApplication{stateManager: state}
	themeService := &tuimocks.FakeThemeService{}
	themeService.GetCurrentThemeReturns(&tuimocks.FakeTheme{})
	app.environmentsView = components.NewEnvironmentsView(styles.NewProvider(themeService), app.loadEnvironments)
	app.SetSessionMover(mover)
	state.SetSessionPlacement(placement)
	return app, state
}

func message(t *testing.T, cmd tea.Cmd) (string, bool) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	switch msg := cmd().(type) {
	case tui.SetStatusEvent:
		return msg.Message, false
	case tui.ShowErrorEvent:
		return msg.Error, msg.Sticky
	default:
		t.Fatalf("got %T, want a status or an error", msg)
	}
	return "", false
}

func TestRequestTransferSaysWhyTheSessionCannotMove(t *testing.T) {
	tests := []struct {
		name      string
		placement tui.SessionPlacement
		inProcess bool
		busy      bool
		target    string
		reason    string
	}{
		{name: "in process", inProcess: true, target: "k8s-dev", reason: "infer chat --daemon"},
		{name: "offline", placement: tui.SessionPlacement{Daemon: true, Local: "laptop", Offline: true}, target: "k8s-dev", reason: "unreachable"},
		{name: "during a transfer", placement: tui.SessionPlacement{Daemon: true, Local: "laptop", Transferring: "k8s-desktop"}, target: "k8s-dev", reason: "already running"},
		{name: "on from a remote environment", placement: sessionOnK8sDev, target: "k8s-desktop", reason: "moves only back to laptop"},
		{name: "where it runs", placement: sessionOnLaptop, target: "laptop", reason: "already runs on laptop"},
		{name: "during a turn", placement: sessionOnLaptop, busy: true, target: "k8s-dev", reason: "Wait for the turn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mover := &tuimocks.FakeSessionMover{}
			var app *ChatApplication
			var state *statemanager.Store
			if tt.inProcess {
				app, state = newDaemonChatApp(tt.placement, nil)
			} else {
				app, state = newDaemonChatApp(tt.placement, mover)
			}
			if tt.busy {
				_ = state.StartChatSession("req-1", "m", make(chan agentdomain.ChatEvent))
			}

			text, _ := message(t, app.requestTransfer(tui.TransferRequestedEvent{Target: tt.target}))

			if !strings.Contains(text, tt.reason) {
				t.Errorf("refusal = %q, want it to say %q", text, tt.reason)
			}
			if mover.TransferCallCount() != 0 {
				t.Error("a refused transfer reached the daemon")
			}
		})
	}
}

func TestTransferShowsItsStagesAndWhereTheSessionLanded(t *testing.T) {
	mover := &tuimocks.FakeSessionMover{}
	moving := tui.SessionPlacement{Daemon: true, Local: "laptop", Transferring: "k8s-dev"}
	mover.PlacementReturns(moving)
	app, state := newDaemonChatApp(sessionOnLaptop, mover)

	text, _ := message(t, app.requestTransfer(tui.TransferRequestedEvent{Target: "k8s-dev", Files: []string{"notes.txt"}}))

	if target, files := mover.TransferArgsForCall(0); target != "k8s-dev" || !slices.Equal(files, []string{"notes.txt"}) {
		t.Fatalf("Transfer(%q, %v), want k8s-dev with notes.txt", target, files)
	}
	if !strings.Contains(text, "Moving session to k8s-dev") || state.GetSessionPlacement().Transferring != "k8s-dev" {
		t.Fatalf("status %q with placement %+v, want the move under way", text, state.GetSessionPlacement())
	}
	if !app.isInputBlocked(tui.ViewStateChat) {
		t.Error("input stays open while the session moves")
	}

	stage := app.transferProgressed(agentdomain.TransferProgress{Stage: agentdomain.TransferCheckpointing})
	if status, ok := stage().(tui.SetStatusEvent); !ok || !strings.Contains(status.Message, "checkpointing") || status.Progress == nil || status.Progress.Current != 2 {
		t.Errorf("checkpointing showed %+v, want stage 2 of 4", stage())
	}

	landed, _ := message(t, app.placementChanged(tui.SessionPlacementChangedEvent{Placement: sessionOnK8sDev, Moved: true, CancelledInterrupts: 1}))
	if !strings.Contains(landed, "now runs on k8s-dev") || !strings.Contains(landed, "1 open prompt") || state.GetSessionPlacement() != sessionOnK8sDev {
		t.Errorf("landing said %q with placement %+v", landed, state.GetSessionPlacement())
	}
}

func TestTransferFailureStaysOnScreen(t *testing.T) {
	app, _ := newDaemonChatApp(tui.SessionPlacement{Daemon: true, Local: "laptop", Transferring: "k8s-dev"}, &tuimocks.FakeSessionMover{})

	text, sticky := message(t, app.transferProgressed(agentdomain.TransferProgress{Stage: agentdomain.TransferFailed, Message: "no capacity"}))

	if !sticky || !strings.Contains(text, "no capacity") || !strings.Contains(text, "stays on laptop") {
		t.Errorf("failure = %q (sticky %v), want a sticky error saying the session stays", text, sticky)
	}
}

func TestTransferStagesNameTheWayBack(t *testing.T) {
	back := tui.SessionPlacement{Daemon: true, Local: "laptop", Environment: "k8s-dev", Transferring: "laptop"}
	app, _ := newDaemonChatApp(back, &tuimocks.FakeSessionMover{})

	status, ok := app.transferProgressed(agentdomain.TransferProgress{Stage: agentdomain.TransferCheckpointing})().(tui.SetStatusEvent)

	if !ok || !strings.Contains(status.Message, "packing the session on k8s-dev") {
		t.Errorf("status = %+v, want the pod packing the session", status)
	}
}

func TestEscCancelsARunningTransfer(t *testing.T) {
	mover := &tuimocks.FakeSessionMover{}
	app, _ := newDaemonChatApp(tui.SessionPlacement{Daemon: true, Local: "laptop", Transferring: "k8s-dev"}, mover)

	app.handleChatViewKeyPress(tea.KeyPressMsg{Code: tea.KeyEscape})

	if mover.CancelTransferCallCount() != 1 {
		t.Error("esc did not cancel the running transfer")
	}
}

func TestOpenEnvironmentsNeedsADaemonChat(t *testing.T) {
	app, state := newDaemonChatApp(tui.SessionPlacement{}, nil)
	text, _ := message(t, app.openEnvironments())
	if state.GetCurrentView() != tui.ViewStateChat || !strings.Contains(text, "infer chat --daemon") {
		t.Errorf("an in-process chat opened %v with %q", state.GetCurrentView(), text)
	}

	mover := &tuimocks.FakeSessionMover{}
	daemon, daemonState := newDaemonChatApp(sessionOnLaptop, mover)
	_ = daemon.openEnvironments()
	if daemonState.GetCurrentView() != tui.ViewStateEnvironments || !daemon.environmentsView.IsLoading() {
		t.Fatalf("a daemon chat opened %v, loading %v", daemonState.GetCurrentView(), daemon.environmentsView.IsLoading())
	}
	if daemon.liveCadence() != components.SpinnerFrameInterval {
		t.Error("the probing picker does not animate")
	}
}

func TestTheInputOffersThisCheckoutsFilesOnlyWhereTheSessionReadsThem(t *testing.T) {
	tests := []struct {
		name      string
		placement tui.SessionPlacement
		offered   bool
	}{
		{name: "in process", placement: tui.SessionPlacement{}, offered: true},
		{name: "on the local daemon", placement: sessionOnLaptop, offered: true},
		{name: "on another environment", placement: sessionOnK8sDev},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := &agentdomainmocks.FakeFileService{}
			project.ListProjectFilesReturns([]string{"README.md"}, nil)
			files := localFiles(project, func() tui.SessionPlacement { return tt.placement })

			listed, _ := files.ListProjectFiles()

			if offered := len(listed) == 1 && files.ValidateFile("README.md") == nil; offered != tt.offered {
				t.Errorf("README.md offered: %v, want %v", offered, tt.offered)
			}
		})
	}
}
