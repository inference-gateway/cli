package tools

import (
	"context"
	"fmt"
	"time"

	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// ActivateAppTool switches focus to a specific application
type ActivateAppTool struct {
	config domain.ConfigService
}

// NewActivateAppTool creates a new ActivateApp tool
func NewActivateAppTool(config domain.ConfigService) *ActivateAppTool {
	return &ActivateAppTool{
		config: config,
	}
}

// Definition returns the tool definition for ActivateApp
func (t *ActivateAppTool) Definition() sdk.ChatCompletionTool {
	description := "Activates (brings to foreground/focus) a specific application by its bundle identifier. Use GetFocusedApp first to check the current state, then use this tool to switch to the target app before performing computer use actions. After activation, wait briefly before sending keyboard/mouse commands."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ActivateApp",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"bundle_id": map[string]any{
						"type":        "string",
						"description": "The bundle identifier of the application to activate (e.g., 'org.mozilla.firefox', 'com.google.Chrome', 'com.apple.Terminal'). Common apps: Firefox='org.mozilla.firefox', Chrome='com.google.Chrome', Safari='com.apple.Safari', Terminal='com.apple.Terminal', VSCode='com.microsoft.VSCode'",
					},
				},
				"required": []string{"bundle_id"},
			},
		},
	}
}

// Validate validates ActivateApp arguments
func (t *ActivateAppTool) Validate(args map[string]any) error {
	bundleID, ok := args["bundle_id"].(string)
	if !ok || bundleID == "" {
		return fmt.Errorf("bundle_id is required and must be a non-empty string")
	}
	return nil
}

// Execute executes the ActivateApp tool
func (t *ActivateAppTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	bundleID, ok := args["bundle_id"].(string)
	if !ok {
		return nil, fmt.Errorf("bundle_id must be a string")
	}

	displayProvider, err := display.DetectDisplay()
	if err != nil {
		return nil, fmt.Errorf("failed to detect display: %w", err)
	}

	controller, err := displayProvider.GetController()
	if err != nil {
		return nil, fmt.Errorf("failed to get display controller: %w", err)
	}
	defer func() {
		if err := controller.Close(); err != nil {
			logger.Debug("Failed to close display controller", "error", err)
		}
	}()

	focusManager, ok := controller.(display.FocusManager)
	if !ok {
		return nil, fmt.Errorf("display controller does not support focus management")
	}

	if err := focusManager.ActivateApp(ctx, bundleID); err != nil {
		return nil, fmt.Errorf("failed to activate app '%s': %w (app may not be running)", bundleID, err)
	}

	time.Sleep(300 * time.Millisecond)

	currentApp, err := focusManager.GetFrontmostApp(ctx)
	if err == nil && currentApp == bundleID {
		result := fmt.Sprintf("Successfully activated %s (bundle ID: %s). The application is now in focus.", parseAppName(bundleID), bundleID)
		return &domain.ToolExecutionResult{
			ToolName: "ActivateApp",
			Success:  true,
			Data: map[string]any{
				"bundle_id": bundleID,
				"app_name":  parseAppName(bundleID),
				"message":   result,
			},
		}, nil
	}

	result := fmt.Sprintf("Attempted to activate %s (bundle ID: %s)", parseAppName(bundleID), bundleID)
	return &domain.ToolExecutionResult{
		ToolName: "ActivateApp",
		Success:  true,
		Data: map[string]any{
			"bundle_id": bundleID,
			"app_name":  parseAppName(bundleID),
			"message":   result,
		},
	}, nil
}

// IsEnabled returns whether the tool is enabled
func (t *ActivateAppTool) IsEnabled() bool {
	return t.config.GetConfig().ComputerUse.Enabled
}

// FormatPreview formats the result for display preview
func (t *ActivateAppTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to activate app"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Activated app"
	}
	if appName, ok := data["app_name"].(string); ok {
		return fmt.Sprintf("Activated: %s", appName)
	}
	return "Activated app"
}

// FormatForLLM formats the result for LLM consumption
func (t *ActivateAppTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Successfully activated application"
	}
	if message, ok := data["message"].(string); ok {
		return message
	}
	return "Successfully activated application"
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *ActivateAppTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *ActivateAppTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *ActivateAppTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
