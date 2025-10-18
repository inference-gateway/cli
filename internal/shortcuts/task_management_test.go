package shortcuts

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestTaskManagementShortcut(t *testing.T) {
	// Test with A2A enabled
	configWithA2A := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
		},
	}

	shortcut := NewTaskManagementShortcut(configWithA2A)

	// Test basic properties
	if shortcut.GetName() != "tasks" {
		t.Errorf("Expected name 'tasks', got '%s'", shortcut.GetName())
	}

	if shortcut.GetUsage() != "/tasks" {
		t.Errorf("Expected usage '/tasks', got '%s'", shortcut.GetUsage())
	}

	// Test can execute with no args
	if !shortcut.CanExecute([]string{}) {
		t.Error("Expected to be able to execute with no args")
	}

	// Test can't execute with args
	if shortcut.CanExecute([]string{"arg"}) {
		t.Error("Expected not to be able to execute with args")
	}

	// Test execution with A2A enabled
	result, err := shortcut.Execute(context.Background(), []string{})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("Expected successful execution")
	}

	if result.SideEffect != SideEffectShowTaskManagement {
		t.Errorf("Expected SideEffectShowTaskManagement, got %v", result.SideEffect)
	}
}

func TestTaskManagementShortcutDisabled(t *testing.T) {
	// Test with A2A disabled
	configWithoutA2A := &config.Config{
		A2A: config.A2AConfig{
			Enabled: false,
		},
	}

	shortcut := NewTaskManagementShortcut(configWithoutA2A)

	// Test execution with A2A disabled
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
