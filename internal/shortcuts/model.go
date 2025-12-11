package shortcuts

import (
	"context"
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
)

// ModelSwitchData contains data for temporary model switching
type ModelSwitchData struct {
	TargetModel   string
	OriginalModel string
	Prompt        string
}

// ModelShortcut executes a prompt with a temporary model switch
type ModelShortcut struct {
	modelService domain.ModelService
}

// NewModelShortcut creates a new ModelShortcut instance
func NewModelShortcut(modelService domain.ModelService) *ModelShortcut {
	return &ModelShortcut{modelService: modelService}
}

func (m *ModelShortcut) GetName() string {
	return "model"
}

func (m *ModelShortcut) GetDescription() string {
	return "Execute a prompt with a specific model (temporary switch)"
}

func (m *ModelShortcut) GetUsage() string {
	return "/model <model-name> <prompt>"
}

func (m *ModelShortcut) CanExecute(args []string) bool {
	// Need at least 2 args: model name and prompt
	return len(args) >= 2
}

func (m *ModelShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) < 2 {
		return ShortcutResult{
			Output:  fmt.Sprintf("Usage: %s", m.GetUsage()),
			Success: false,
		}, nil
	}

	targetModel := args[0]
	prompt := strings.Join(args[1:], " ")

	// Validate target model exists
	if err := m.modelService.ValidateModel(targetModel); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Invalid model '%s': %v", targetModel, err),
			Success: false,
		}, nil
	}

	// Get current model for restoration
	originalModel := m.modelService.GetCurrentModel()
	if originalModel == "" {
		return ShortcutResult{
			Output:  "No model currently selected",
			Success: false,
		}, nil
	}

	// Return side effect with all necessary data
	return ShortcutResult{
		Output:     "", // No output - the side effect handler will add the message
		Success:    true,
		SideEffect: SideEffectSendMessageWithModel,
		Data: ModelSwitchData{
			TargetModel:   targetModel,
			OriginalModel: originalModel,
			Prompt:        prompt,
		},
	}, nil
}
