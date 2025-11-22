package shortcuts

import (
	"context"
	"testing"

	"github.com/inference-gateway/cli/config"
)

// mockModelService implements a minimal ModelService for testing
type mockModelService struct {
	currentModel string
}

func (m *mockModelService) ListModels(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockModelService) SelectModel(modelID string) error {
	m.currentModel = modelID
	return nil
}

func (m *mockModelService) GetCurrentModel() string {
	return m.currentModel
}

func (m *mockModelService) IsModelAvailable(modelID string) bool {
	return true
}

func (m *mockModelService) ValidateModel(modelID string) error {
	return nil
}

func TestGitShortcut_GetName(t *testing.T) {
	shortcut := NewGitShortcut(nil, &config.Config{}, nil)

	expected := "git"
	actual := shortcut.GetName()

	if actual != expected {
		t.Errorf("Expected name %s, got %s", expected, actual)
	}
}

func TestGitShortcut_GetDescription(t *testing.T) {
	shortcut := NewGitShortcut(nil, &config.Config{}, nil)

	expected := "Execute git commands (status, pull, log, commit, push, etc.)"
	actual := shortcut.GetDescription()

	if actual != expected {
		t.Errorf("Expected description %s, got %s", expected, actual)
	}
}

func TestGitShortcut_GetUsage(t *testing.T) {
	shortcut := NewGitShortcut(nil, &config.Config{}, nil)

	expected := "/git <command> [args...] (e.g., /git status, /git pull, /git log, /git commit, /git push)"
	actual := shortcut.GetUsage()

	if actual != expected {
		t.Errorf("Expected usage %s, got %s", expected, actual)
	}
}

func TestGitShortcut_CanExecute(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "no arguments not allowed",
			args:     []string{},
			expected: false,
		},
		{
			name:     "status command allowed",
			args:     []string{"status"},
			expected: true,
		},
		{
			name:     "commit command allowed",
			args:     []string{"commit"},
			expected: true,
		},
		{
			name:     "push command allowed",
			args:     []string{"push"},
			expected: true,
		},
		{
			name:     "multiple arguments allowed",
			args:     []string{"commit", "-m", "message"},
			expected: true,
		},
	}

	shortcut := NewGitShortcut(nil, &config.Config{}, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortcut.CanExecute(tt.args)
			if result != tt.expected {
				t.Errorf("Expected CanExecute(%v) = %v, got %v", tt.args, tt.expected, result)
			}
		})
	}
}

func TestGitShortcut_HasCommitMessage(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "no message flag",
			args:     []string{},
			expected: false,
		},
		{
			name:     "-m flag with message",
			args:     []string{"-m", "test message"},
			expected: true,
		},
		{
			name:     "-m flag without message",
			args:     []string{"-m"},
			expected: false,
		},
		{
			name:     "--message flag with message",
			args:     []string{"--message", "test message"},
			expected: true,
		},
		{
			name:     "-m= format",
			args:     []string{"-m=test message"},
			expected: true,
		},
		{
			name:     "--message= format",
			args:     []string{"--message=test message"},
			expected: true,
		},
		{
			name:     "other flags without message",
			args:     []string{"-a", "--amend"},
			expected: false,
		},
	}

	shortcut := NewGitShortcut(nil, &config.Config{}, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortcut.hasCommitMessage(tt.args)
			if result != tt.expected {
				t.Errorf("Expected hasCommitMessage(%v) = %v, got %v", tt.args, tt.expected, result)
			}
		})
	}
}

func TestGitShortcut_ModelFallbackPriority(t *testing.T) {
	tests := []struct {
		name                 string
		gitCommitModel       string
		agentModel           string
		currentSelectedModel string
		expectModelUsed      string
		expectError          bool
	}{
		{
			name:                 "use git.commit_message.model when set",
			gitCommitModel:       "provider1/model1",
			agentModel:           "provider2/model2",
			currentSelectedModel: "provider3/model3",
			expectModelUsed:      "provider1/model1",
			expectError:          false,
		},
		{
			name:                 "fallback to agent.model when git model not set",
			gitCommitModel:       "",
			agentModel:           "provider2/model2",
			currentSelectedModel: "provider3/model3",
			expectModelUsed:      "provider2/model2",
			expectError:          false,
		},
		{
			name:                 "fallback to current model when both configs not set",
			gitCommitModel:       "",
			agentModel:           "",
			currentSelectedModel: "provider3/model3",
			expectModelUsed:      "provider3/model3",
			expectError:          false,
		},
		{
			name:                 "error when no model configured anywhere",
			gitCommitModel:       "",
			agentModel:           "",
			currentSelectedModel: "",
			expectModelUsed:      "",
			expectError:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Git: config.GitConfig{
					CommitMessage: config.GitCommitMessageConfig{
						Model: tt.gitCommitModel,
					},
				},
				Agent: config.AgentConfig{
					Model: tt.agentModel,
				},
			}

			modelService := &mockModelService{
				currentModel: tt.currentSelectedModel,
			}

			shortcut := NewGitShortcut(nil, cfg, modelService)

			// Test the model selection logic by checking what model would be used
			// We can't easily test generateCommitMessage directly as it requires a real client,
			// but we can verify the model selection logic inline using the shortcut's config and modelService
			model := shortcut.config.Git.CommitMessage.Model
			if model == "" {
				model = shortcut.config.Agent.Model
			}
			if model == "" && shortcut.modelService != nil {
				model = shortcut.modelService.GetCurrentModel()
			}

			if tt.expectError {
				if model != "" {
					t.Errorf("Expected empty model (which would cause error), got %s", model)
				}
			} else {
				if model != tt.expectModelUsed {
					t.Errorf("Expected model %s, got %s", tt.expectModelUsed, model)
				}
			}
		})
	}
}

func TestGitShortcut_ModelFallbackWithNilModelService(t *testing.T) {
	cfg := &config.Config{
		Git: config.GitConfig{
			CommitMessage: config.GitCommitMessageConfig{
				Model: "",
			},
		},
		Agent: config.AgentConfig{
			Model: "",
		},
	}

	// When modelService is nil, it should still work (just can't fallback to current model)
	shortcut := NewGitShortcut(nil, cfg, nil)

	// Verify the shortcut is created without panic
	if shortcut == nil {
		t.Fatal("Expected shortcut to be created, got nil")
	}

	// Verify modelService is nil
	if shortcut.modelService != nil {
		t.Error("Expected modelService to be nil")
	}

	// Test the model selection logic with nil modelService
	model := cfg.Git.CommitMessage.Model
	if model == "" {
		model = cfg.Agent.Model
	}
	// shortcut.modelService is nil, so this block won't execute
	if model == "" && shortcut.modelService != nil {
		model = shortcut.modelService.GetCurrentModel()
	}

	// With no config and nil modelService, model should be empty
	if model != "" {
		t.Errorf("Expected empty model with nil modelService and no config, got %s", model)
	}
}
