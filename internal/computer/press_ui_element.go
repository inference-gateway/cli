package computer

import (
	"context"
	"errors"
	"fmt"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// PressUIElementTool invokes an accessibility element's default press action
// directly, without moving the cursor - no physical-mouse interference and no
// coordinate risk for launches and standard controls.
type PressUIElementTool struct {
	config *config.Config
}

// NewPressUIElementTool creates a new PressUIElement tool
func NewPressUIElementTool(cfg *config.Config) *PressUIElementTool {
	return &PressUIElementTool{config: cfg}
}

// Definition returns the tool definition for PressUIElement
func (t *PressUIElementTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.PressUIElement.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "PressUIElement",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"label": map[string]any{
						"type":        "string",
						"description": "The element's title exactly as shown by GetUIElements (the quoted text, e.g. 'Finder' or 'Save'). The first exact match in the tree is pressed.",
					},
					"target": map[string]any{
						"type":        "string",
						"enum":        []string{"frontmost", "dock", "menubar"},
						"description": "Which accessibility tree to search: 'frontmost' (default), 'dock', or 'menubar'. Use the same target that listed the element.",
					},
				},
				"required": []string{"label"},
			},
		},
	}
}

// Validate validates PressUIElement arguments
func (t *PressUIElementTool) Validate(args map[string]any) error {
	label, _ := args["label"].(string)
	if label == "" {
		return fmt.Errorf("label is required")
	}
	target, _ := args["target"].(string)
	if !axTargets[target] {
		return fmt.Errorf("invalid target %q: must be one of frontmost, dock, menubar", target)
	}
	return nil
}

// Execute executes the PressUIElement tool
func (t *PressUIElementTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	label, _ := args["label"].(string)
	target, _ := args["target"].(string)
	if target == "" {
		target = "frontmost"
	}

	if err := acquireScreenLock(); err != nil {
		return nil, err
	}

	provider, err := display.DetectAXProvider()
	if err != nil {
		return axDegradeResult("PressUIElement", "Nothing was pressed: "+axNoProviderMessage+
			" Then click with MouseClick at the element's coordinates."), nil
	}

	err = provider.PressElement(ctx, target, label)
	switch {
	case errors.Is(err, display.ErrNoAXPermission):
		return axDegradeResult("PressUIElement", "Nothing was pressed: "+axNoPermissionMessage+
			" Then click with MouseClick at the element's coordinates."), nil
	case errors.Is(err, display.ErrElementNotFound):
		return nil, fmt.Errorf("no UI element titled %q in %s - run GetUIElements to see the current elements", label, target)
	case err != nil:
		return nil, err
	}

	return &domain.ToolExecutionResult{
		ToolName: "PressUIElement",
		Success:  true,
		Data: map[string]any{
			"label":   label,
			"target":  target,
			"message": fmt.Sprintf("Pressed UI element %q in %s", label, target),
		},
	}, nil
}

// IsEnabled returns whether the tool is enabled
func (t *PressUIElementTool) IsEnabled() bool {
	return t.config.ComputerUse.Enabled && t.config.ComputerUse.Tools.PressUIElement.Enabled
}

// FormatPreview formats the result for display preview
func (t *PressUIElementTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to press UI element"
	}
	if data, ok := result.Data.(map[string]any); ok {
		if label, ok := data["label"].(string); ok {
			return fmt.Sprintf("Pressed %q", label)
		}
	}
	return "Pressed UI element"
}

// FormatForLLM formats the result for LLM consumption
func (t *PressUIElementTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	return axMessageForLLM(result, "Successfully pressed UI element")
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *PressUIElementTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *PressUIElementTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *PressUIElementTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
