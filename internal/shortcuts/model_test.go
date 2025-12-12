package shortcuts

import (
	"context"
	"fmt"
	"testing"
)

// MockModelService for testing
type mockModelService struct {
	currentModel    string
	availableModels []string
	validateErr     error
}

func (m *mockModelService) ListModels(ctx context.Context) ([]string, error) {
	return m.availableModels, nil
}

func (m *mockModelService) SelectModel(modelID string) error {
	m.currentModel = modelID
	return nil
}

func (m *mockModelService) GetCurrentModel() string {
	return m.currentModel
}

func (m *mockModelService) IsModelAvailable(modelID string) bool {
	for _, model := range m.availableModels {
		if model == modelID {
			return true
		}
	}
	return false
}

func (m *mockModelService) ValidateModel(modelID string) error {
	if m.validateErr != nil {
		return m.validateErr
	}
	if !m.IsModelAvailable(modelID) {
		return fmt.Errorf("model '%s' not available", modelID)
	}
	return nil
}

func (m *mockModelService) IsVisionModel(modelID string) bool {
	return false
}

func TestModelShortcut_GetName(t *testing.T) {
	modelService := &mockModelService{}
	shortcut := NewModelShortcut(modelService)

	if shortcut.GetName() != "model" {
		t.Errorf("Expected name 'model', got '%s'", shortcut.GetName())
	}
}

func TestModelShortcut_GetUsage(t *testing.T) {
	modelService := &mockModelService{}
	shortcut := NewModelShortcut(modelService)

	expected := "/model <model-name> <prompt>"
	if shortcut.GetUsage() != expected {
		t.Errorf("Expected usage '%s', got '%s'", expected, shortcut.GetUsage())
	}
}

func TestModelShortcut_CanExecute(t *testing.T) {
	modelService := &mockModelService{}
	shortcut := NewModelShortcut(modelService)

	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "valid args - model and prompt",
			args:     []string{"claude-opus-4", "hello"},
			expected: true,
		},
		{
			name:     "valid args - model and multi-word prompt",
			args:     []string{"claude-opus-4", "hello", "world", "test"},
			expected: true,
		},
		{
			name:     "invalid args - only model",
			args:     []string{"claude-opus-4"},
			expected: false,
		},
		{
			name:     "invalid args - empty",
			args:     []string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortcut.CanExecute(tt.args)
			if result != tt.expected {
				t.Errorf("CanExecute() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestModelShortcut_Execute(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		currentModel   string
		validateErr    error
		wantSuccess    bool
		wantSideEffect SideEffectType
		checkData      bool
	}{
		{
			name:           "successful execution with simple prompt",
			args:           []string{"claude-opus-4", "hello"},
			currentModel:   "claude-sonnet-4",
			wantSuccess:    true,
			wantSideEffect: SideEffectSendMessageWithModel,
			checkData:      true,
		},
		{
			name:           "successful execution with multi-word prompt",
			args:           []string{"claude-opus-4", "hello", "world", "test"},
			currentModel:   "claude-sonnet-4",
			wantSuccess:    true,
			wantSideEffect: SideEffectSendMessageWithModel,
			checkData:      true,
		},
		{
			name:           "invalid model error",
			args:           []string{"invalid-model", "test"},
			currentModel:   "claude-sonnet-4",
			validateErr:    fmt.Errorf("model not found"),
			wantSuccess:    false,
			wantSideEffect: SideEffectNone,
			checkData:      false,
		},
		{
			name:           "missing prompt error",
			args:           []string{"claude-opus-4"},
			currentModel:   "claude-sonnet-4",
			wantSuccess:    false,
			wantSideEffect: SideEffectNone,
			checkData:      false,
		},
		{
			name:           "no current model error",
			args:           []string{"claude-opus-4", "test"},
			currentModel:   "",
			wantSuccess:    false,
			wantSideEffect: SideEffectNone,
			checkData:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			modelService := &mockModelService{
				currentModel:    tt.currentModel,
				availableModels: []string{"claude-opus-4", "claude-sonnet-4"},
				validateErr:     tt.validateErr,
			}
			shortcut := NewModelShortcut(modelService)

			ctx := context.Background()
			result, err := shortcut.Execute(ctx, tt.args)

			if err != nil {
				t.Errorf("Execute() returned error: %v", err)
			}

			if result.Success != tt.wantSuccess {
				t.Errorf("Execute() success = %v, want %v", result.Success, tt.wantSuccess)
			}

			if result.SideEffect != tt.wantSideEffect {
				t.Errorf("Execute() side effect = %v, want %v", result.SideEffect, tt.wantSideEffect)
			}

			if tt.checkData && result.Data != nil {
				validateModelSwitchData(t, result.Data, tt.args, tt.currentModel)
			}
		})
	}
}

// validateModelSwitchData validates the ModelSwitchData returned from Execute
func validateModelSwitchData(t *testing.T, data any, args []string, currentModel string) {
	t.Helper()

	switchData, ok := data.(ModelSwitchData)
	if !ok {
		t.Errorf("Execute() data is not ModelSwitchData type")
		return
	}

	if switchData.TargetModel != args[0] {
		t.Errorf("Execute() data.TargetModel = %v, want %v", switchData.TargetModel, args[0])
	}

	if switchData.OriginalModel != currentModel {
		t.Errorf("Execute() data.OriginalModel = %v, want %v", switchData.OriginalModel, currentModel)
	}

	expectedPrompt := buildExpectedPrompt(args)
	if switchData.Prompt != expectedPrompt {
		t.Errorf("Execute() data.Prompt = %v, want %v", switchData.Prompt, expectedPrompt)
	}
}

// buildExpectedPrompt constructs the expected prompt from args
func buildExpectedPrompt(args []string) string {
	if len(args) <= 1 {
		return ""
	}

	promptParts := args[1:]
	expectedPrompt := ""
	for i, part := range promptParts {
		if i > 0 {
			expectedPrompt += " "
		}
		expectedPrompt += part
	}
	return expectedPrompt
}
