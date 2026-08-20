package computer

import (
	"context"
	"fmt"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/computer/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// GetFocusedAppTool gets the currently focused application.
type GetFocusedAppTool struct {
	config *config.Config
}

// NewGetFocusedAppTool creates a new GetFocusedApp tool
func NewGetFocusedAppTool(cfg *config.Config) *GetFocusedAppTool {
	return &GetFocusedAppTool{
		config: cfg,
	}
}

// Definition returns the tool definition for GetFocusedApp
func (t *GetFocusedAppTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.GetFocusedApp.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "GetFocusedApp",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

// Validate validates GetFocusedApp arguments
func (t *GetFocusedAppTool) Validate(args map[string]any) error {
	return nil
}

// Execute executes the GetFocusedApp tool
func (t *GetFocusedAppTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	appProvider, err := display.DetectAppProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to detect app provider: %w", err)
	}

	app, err := appProvider.GetFocused(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get focused app: %w", err)
	}

	if app == nil {
		return &domain.ToolExecutionResult{
			ToolName: "GetFocusedApp",
			Success:  true,
			Data: map[string]any{
				"message": "No application is currently focused (headless session or no active windows).",
			},
		}, nil
	}

	msg := fmt.Sprintf("Currently focused application: %s (ID: %s)", app.Name, app.ID)
	if app.PlatformID != "" && app.PlatformID != app.ID {
		msg += fmt.Sprintf(", platform_id: %s", app.PlatformID)
	}

	return &domain.ToolExecutionResult{
		ToolName: "GetFocusedApp",
		Success:  true,
		Data: map[string]any{
			"app_id":      app.ID,
			"app_name":    app.Name,
			"platform_id": app.PlatformID,
			"message":     msg,
		},
	}, nil
}

// IsEnabled returns whether the tool is enabled
func (t *GetFocusedAppTool) IsEnabled() bool {
	return t.config.ComputerUse.Enabled
}

// FormatPreview formats the result for display preview
func (t *GetFocusedAppTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to get focused app"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Got focused app"
	}
	if appName, ok := data["app_name"].(string); ok && appName != "" {
		return fmt.Sprintf("Focused: %s", appName)
	}
	return "No focused app"
}

// FormatForLLM formats the result for LLM consumption
func (t *GetFocusedAppTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Successfully retrieved focused application"
	}
	if message, ok := data["message"].(string); ok {
		return message
	}
	return "Successfully retrieved focused application"
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *GetFocusedAppTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *GetFocusedAppTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *GetFocusedAppTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
