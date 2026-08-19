package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ActivateAppTool brings a running application to the foreground.
//
// The tool takes either:
//   - "app_id": a stable application ID returned by GetFocusedApp
//     (e.g. "com.google.Chrome" on macOS, "pid:1234" on Linux)
//   - "name": a human-readable name substring to match (case-insensitive, first
//     match wins).  At least one of app_id or name is required; app_id wins if
//     both are provided.
type ActivateAppTool struct {
	config *config.Config
}

// NewActivateAppTool creates a new ActivateApp tool
func NewActivateAppTool(cfg *config.Config) *ActivateAppTool {
	return &ActivateAppTool{
		config: cfg,
	}
}

// Definition returns the tool definition for ActivateApp
func (t *ActivateAppTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.ActivateApp.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ActivateApp",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"app_id": map[string]any{
						"type":        "string",
						"description": "The stable application identifier returned by GetFocusedApp (e.g., 'com.google.Chrome', 'pid:1234'). On macOS this is the bundle ID; on Linux it is 'pid:<PID>'. Optional if 'name' is provided.",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "A human-readable application name substring to match (e.g., 'Chrome', 'Terminal', 'VS Code'). Case-insensitive, first match wins. Optional if 'app_id' is provided.",
					},
				},
				"required": []string{},
			},
		},
	}
}

// Validate validates ActivateApp arguments
func (t *ActivateAppTool) Validate(args map[string]any) error {
	appID, _ := args["app_id"].(string)
	name, _ := args["name"].(string)

	if appID == "" && name == "" {
		return fmt.Errorf("either app_id or name is required")
	}
	return nil
}

// Execute executes the ActivateApp tool
func (t *ActivateAppTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	appID, _ := args["app_id"].(string)
	name, _ := args["name"].(string)

	if err := acquireScreenLock(); err != nil {
		return nil, err
	}

	appProvider, err := display.DetectAppProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to detect app provider: %w", err)
	}

	var targetID string

	// If app_id is provided, use it directly; otherwise fall back to name match
	if appID != "" {
		targetID = appID
	} else {
		targetID, err = resolveAppByName(ctx, appProvider, name)
		if err != nil {
			return nil, err
		}
	}

	if err := appProvider.Activate(ctx, targetID); err != nil {
		if errors.Is(err, display.ErrAppNotFound) {
			return nil, fmt.Errorf("application %q is not running (try activating by name instead)", targetID)
		}
		return nil, fmt.Errorf("failed to activate app %q: %w", targetID, err)
	}

	time.Sleep(150 * time.Millisecond)

	// Verify the activation succeeded
	focusedApp, _ := appProvider.GetFocused(ctx)
	data := map[string]any{
		"app_id":  targetID,
		"message": fmt.Sprintf("Successfully activated %s", targetID),
	}

	if focusedApp != nil {
		data["app_name"] = focusedApp.Name
	}

	return &domain.ToolExecutionResult{
		ToolName: "ActivateApp",
		Success:  true,
		Data:     data,
	}, nil
}

// IsEnabled returns whether the tool is enabled
func (t *ActivateAppTool) IsEnabled() bool {
	return t.config.ComputerUse.Enabled
}

// resolveAppByName looks up a running application by name substring match.
func resolveAppByName(ctx context.Context, ap display.AppProvider, name string) (string, error) {
	apps, err := ap.ListRunning(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list running apps: %w", err)
	}

	nameLower := strings.ToLower(name)
	for _, app := range apps {
		if strings.Contains(strings.ToLower(app.Name), nameLower) ||
			strings.Contains(strings.ToLower(app.ID), nameLower) {
			return app.ID, nil
		}
	}

	return "", fmt.Errorf("no running application matched name %q", name)
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
	if appID, ok := data["app_id"].(string); ok {
		return fmt.Sprintf("Activated: %s", appID)
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
