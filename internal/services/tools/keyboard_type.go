package tools

import (
	"context"
	"fmt"
	"time"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// KeyboardTypeTool types text or sends key combinations
type KeyboardTypeTool struct {
	config          *config.Config
	enabled         bool
	formatter       domain.BaseFormatter
	rateLimiter     domain.RateLimiter
	displayProvider display.Provider
	stateManager    domain.StateManager
}

// NewKeyboardTypeTool creates a new keyboard type tool
func NewKeyboardTypeTool(cfg *config.Config, rateLimiter domain.RateLimiter, displayProvider display.Provider, stateManager domain.StateManager) *KeyboardTypeTool {
	return &KeyboardTypeTool{
		config:          cfg,
		enabled:         cfg.ComputerUse.Enabled && cfg.ComputerUse.KeyboardType.Enabled,
		formatter:       domain.NewBaseFormatter("KeyboardType"),
		rateLimiter:     rateLimiter,
		displayProvider: displayProvider,
		stateManager:    stateManager,
	}
}

// Definition returns the tool definition for the LLM
func (t *KeyboardTypeTool) Definition() sdk.ChatCompletionTool {
	description := "Types text or sends key combinations INTO GUI APPLICATIONS at the current cursor position (e.g., typing in a text editor, browser search box, or form field). DO NOT use this to run shell commands - use the Bash tool instead. To open applications on macOS, use Bash with 'open -a AppName'. Requires user approval unless in auto-accept mode. Note: Exactly one of 'text' or 'key_combo' must be provided."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "KeyboardType",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "Text to type into the active GUI application (NOT for running commands).",
					},
					"key_combo": map[string]any{
						"type":        "string",
						"description": "Key combination to send (e.g., 'cmd+c' for copy, 'cmd+v' for paste, 'cmd+tab' to switch apps). Use platform-specific modifiers: 'cmd' on macOS, 'ctrl' on Linux/Windows.",
					},
				},
			},
		},
	}
}

// Execute runs the keyboard type tool with given arguments
func (t *KeyboardTypeTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.rateLimiter.CheckAndRecord("KeyboardType"); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	text, hasText := args["text"].(string)
	keyCombo, hasKeyCombo := args["key_combo"].(string)

	if !hasText && !hasKeyCombo {
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "either 'text' or 'key_combo' must be provided",
		}, nil
	}

	if hasText && hasKeyCombo {
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "only one of 'text' or 'key_combo' can be provided",
		}, nil
	}

	if t.displayProvider == nil {
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "no compatible display platform detected",
		}, nil
	}

	controller, err := t.displayProvider.GetController()
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("failed to get platform controller: %v", err),
		}, nil
	}
	defer func() {
		if closeErr := controller.Close(); closeErr != nil {
			logger.Warn("Failed to close controller", "error", closeErr)
		}
	}()

	if t.stateManager != nil {
		t.restoreInputFocus(ctx, controller)
	}

	var execErr error
	if hasText {
		execErr = controller.TypeText(ctx, text, t.config.ComputerUse.KeyboardType.TypingDelayMs)
	} else {
		execErr = controller.SendKeyCombo(ctx, keyCombo)
	}

	if execErr != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("keyboard action failed: %v", execErr),
		}, nil
	}

	result := domain.KeyboardTypeToolResult{
		Text:     text,
		KeyCombo: keyCombo,
		Method:   t.displayProvider.GetDisplayInfo().Name,
	}

	return &domain.ToolExecutionResult{
		ToolName:  "KeyboardType",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *KeyboardTypeTool) Validate(args map[string]any) error {
	text, hasText := args["text"].(string)
	keyCombo, hasKeyCombo := args["key_combo"].(string)

	if !hasText && !hasKeyCombo {
		return fmt.Errorf("either 'text' or 'key_combo' must be provided")
	}

	if hasText && hasKeyCombo {
		return fmt.Errorf("only one of 'text' or 'key_combo' can be provided")
	}

	if hasText {
		if len(text) > t.config.ComputerUse.KeyboardType.MaxTextLength {
			return fmt.Errorf("text length exceeds maximum of %d characters", t.config.ComputerUse.KeyboardType.MaxTextLength)
		}
		if len(text) == 0 {
			return fmt.Errorf("text cannot be empty")
		}
	}

	if hasKeyCombo {
		if len(keyCombo) == 0 {
			return fmt.Errorf("key_combo cannot be empty")
		}
	}

	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *KeyboardTypeTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results for different contexts
func (t *KeyboardTypeTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *KeyboardTypeTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Keyboard input failed"
	}
	data, ok := result.Data.(domain.KeyboardTypeToolResult)
	if !ok {
		return "Keyboard input sent"
	}
	if data.Text != "" {
		preview := data.Text
		if len(preview) > 30 {
			preview = preview[:27] + "..."
		}
		return fmt.Sprintf("Typed: %s", preview)
	}
	return fmt.Sprintf("Key combo: %s", data.KeyCombo)
}

// FormatForLLM formats the result for LLM consumption
func (t *KeyboardTypeTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(domain.KeyboardTypeToolResult)
	if !ok {
		return "Keyboard input sent successfully"
	}
	if data.Text != "" {
		return fmt.Sprintf("Typed text: '%s' using %s", data.Text, data.Method)
	}
	return fmt.Sprintf("Sent key combination '%s' using %s", data.KeyCombo, data.Method)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *KeyboardTypeTool) ShouldCollapseArg(key string) bool {
	return key == "text"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *KeyboardTypeTool) ShouldAlwaysExpand() bool {
	return false
}

func (t *KeyboardTypeTool) restoreInputFocus(ctx context.Context, controller display.DisplayController) {
	clickX, clickY := t.stateManager.GetLastClickCoordinates()
	if clickX <= 0 && clickY <= 0 {
		return
	}

	lastFocusedApp := t.stateManager.GetLastFocusedApp()
	if lastFocusedApp != "" {
		t.activateLastFocusedApp(ctx, controller, lastFocusedApp)
	}

	t.reClickInputField(ctx, controller, clickX, clickY)
}

func (t *KeyboardTypeTool) activateLastFocusedApp(ctx context.Context, controller display.DisplayController, appID string) {
	focusManager, ok := controller.(display.FocusManager)
	if !ok {
		return
	}

	if err := focusManager.ActivateApp(ctx, appID); err != nil {
		logger.Warn("Failed to restore app focus", "app_id", appID, "error", err)
		return
	}

	time.Sleep(100 * time.Millisecond)
}

func (t *KeyboardTypeTool) reClickInputField(ctx context.Context, controller display.DisplayController, x, y int) {
	if err := controller.MoveMouse(ctx, x, y); err != nil {
		logger.Warn("Failed to move mouse to stored coordinates", "x", x, "y", y, "error", err)
		return
	}

	mouseButton := display.ParseMouseButton("left")
	if err := controller.ClickMouse(ctx, mouseButton, 1); err != nil {
		logger.Warn("Failed to re-click input field", "error", err)
		return
	}

	time.Sleep(100 * time.Millisecond)
}
