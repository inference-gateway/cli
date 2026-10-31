package shortcuts

import (
	"slices"
	"testing"

	config "github.com/inference-gateway/cli/config"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

func TestWorkspacesShortcutOpensThePicker(t *testing.T) {
	shortcut := NewWorkspacesShortcut()

	result, err := shortcut.Execute(t.Context(), nil)

	if err != nil || result.SideEffect != SideEffectShowWorkspaces {
		t.Fatalf("Execute = %+v, %v, want the picker", result, err)
	}
	if shortcut.CanExecute([]string{"k8s-dev"}) {
		t.Error("/workspaces took an argument")
	}
}

func TestTeleportShortcutTeleportsTheSessionOrOpensThePicker(t *testing.T) {
	shortcut := NewTeleportShortcut(nil)

	open, _ := shortcut.Execute(t.Context(), nil)
	if open.SideEffect != SideEffectShowWorkspaces {
		t.Errorf("/teleport without a target = %+v, want the picker", open)
	}

	teleport, _ := shortcut.Execute(t.Context(), []string{"k8s-dev", "notes.txt", ".env.example"})
	request, ok := teleport.Data.(tui.TeleportRequestedEvent)
	if teleport.SideEffect != SideEffectTeleportSession || !ok || request.Target != "k8s-dev" || !slices.Equal(request.Files, []string{"notes.txt", ".env.example"}) {
		t.Errorf("/teleport k8s-dev notes.txt .env.example = %+v, want a teleport carrying both files", teleport)
	}
}

func TestTeleportShortcutCompletesTheConfiguredWorkspaces(t *testing.T) {
	shortcut := NewTeleportShortcut([]config.WorkspaceEntry{
		{Name: "k8s-dev", Kind: config.WorkspaceKindKubernetes},
		{Name: "workstation", Kind: config.WorkspaceKindDaemon},
	})

	var names []string
	for _, sub := range shortcut.GetSubcommands() {
		names = append(names, sub.Name)
	}

	if !slices.Equal(names, []string{"k8s-dev", "workstation"}) {
		t.Errorf("completions = %v, want the workspaces daemon.yaml names", names)
	}
}
