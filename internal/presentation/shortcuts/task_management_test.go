package shortcuts

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestA2ATaskManagementShortcut(t *testing.T) {
	configWithA2A := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
		},
	}

	shortcut := NewA2ATaskManagementShortcut(configWithA2A)

	if shortcut.GetName() != "tasks" {
		t.Errorf("Expected name 'tasks', got '%s'", shortcut.GetName())
	}

	if shortcut.GetUsage() != "/tasks" {
		t.Errorf("Expected usage '/tasks', got '%s'", shortcut.GetUsage())
	}

	if !shortcut.CanExecute([]string{}) {
		t.Error("Expected to be able to execute with no args")
	}

	if shortcut.CanExecute([]string{"arg"}) {
		t.Error("Expected not to be able to execute with args")
	}

	result, err := shortcut.Execute(context.Background(), []string{})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("Expected successful execution")
	}

	if result.SideEffect != SideEffectShowA2ATaskManagement {
		t.Errorf("Expected SideEffectShowA2ATaskManagement, got %v", result.SideEffect)
	}
}

func TestA2ATaskManagementShortcutDisabled(t *testing.T) {
	configWithoutA2A := &config.Config{
		A2A: config.A2AConfig{
			Enabled: false,
		},
	}

	shortcut := NewA2ATaskManagementShortcut(configWithoutA2A)

	result, err := shortcut.Execute(context.Background(), []string{})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result.Success {
		t.Error("Expected execution to fail when A2A is disabled")
	}

	if result.Output == "" {
		t.Error("Expected error message when A2A is disabled")
	}
}
