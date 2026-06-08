package shortcuts

import (
	"context"
	"fmt"
	"strings"
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

func TestSwitchShortcut_GetName(t *testing.T) {
	modelService := &mockModelService{}
	shortcut := NewSwitchShortcut(modelService)

	if shortcut.GetName() != "model" {
		t.Errorf("Expected name 'model', got '%s'", shortcut.GetName())
	}
}

func TestSwitchShortcut_GetUsage(t *testing.T) {
	modelService := &mockModelService{}
	shortcut := NewSwitchShortcut(modelService)

	expected := "/model [model-name] [prompt]"
	if shortcut.GetUsage() != expected {
		t.Errorf("Expected usage '%s', got '%s'", expected, shortcut.GetUsage())
	}
}

func TestSwitchShortcut_CanExecute(t *testing.T) {
	modelService := &mockModelService{}
	shortcut := NewSwitchShortcut(modelService)

	tests := []struct {
		name string
		args []string
	}{
		{name: "no args", args: []string{}},
		{name: "model only", args: []string{"claude-opus-4"}},
		{name: "model and prompt", args: []string{"claude-opus-4", "hello"}},
		{name: "model and multi-word prompt", args: []string{"claude-opus-4", "hello", "world", "test"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortcut.CanExecute(tt.args)
			if !result {
				t.Errorf("CanExecute() = false, want true for args %v", tt.args)
			}
		})
	}
}

func TestSwitchShortcut_Execute_NoArgs(t *testing.T) {
	modelService := &mockModelService{
		currentModel:    "claude-sonnet-4",
		availableModels: []string{"claude-sonnet-4"},
	}
	shortcut := NewSwitchShortcut(modelService)

	ctx := context.Background()
	result, err := shortcut.Execute(ctx, []string{})

	if err != nil {
		t.Errorf("Execute() returned error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute() success = false, want true")
	}

	if result.SideEffect != SideEffectSwitchModel {
		t.Errorf("Execute() side effect = %v, want %v", result.SideEffect, SideEffectSwitchModel)
	}

	if !strings.Contains(result.Output, "Select a model from the dropdown") {
		t.Errorf("Execute() output = %q, want it to mention model dropdown", result.Output)
	}
}

func TestSwitchShortcut_Execute_PermanentSwitch(t *testing.T) {
	modelService := &mockModelService{
		currentModel:    "claude-sonnet-4",
		availableModels: []string{"claude-sonnet-4", "claude-opus-4"},
	}
	shortcut := NewSwitchShortcut(modelService)

	ctx := context.Background()
	result, err := shortcut.Execute(ctx, []string{"claude-opus-4"})

	if err != nil {
		t.Errorf("Execute() returned error: %v", err)
	}

	if !result.Success {
		t.Errorf("Execute() success = false, want true")
	}

	if result.SideEffect != SideEffectSwitchModel {
		t.Errorf("Execute() side effect = %v, want %v", result.SideEffect, SideEffectSwitchModel)
	}

	if !strings.Contains(result.Output, "claude-opus-4") {
		t.Errorf("Execute() output = %q, want it to mention claude-opus-4", result.Output)
	}

	if modelService.currentModel != "claude-opus-4" {
		t.Errorf("currentModel = %q, want %q", modelService.currentModel, "claude-opus-4")
	}
}

func TestSwitchShortcut_Execute_WithPrompt(t *testing.T) {
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
			shortcut := NewSwitchShortcut(modelService)

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
