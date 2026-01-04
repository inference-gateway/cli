package tools

import (
	"context"
	"fmt"

	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// GetFocusedAppTool gets the currently focused application
type GetFocusedAppTool struct {
	config domain.ConfigService
}

// NewGetFocusedAppTool creates a new GetFocusedApp tool
func NewGetFocusedAppTool(config domain.ConfigService) *GetFocusedAppTool {
	return &GetFocusedAppTool{
		config: config,
	}
}

// Definition returns the tool definition for GetFocusedApp
func (t *GetFocusedAppTool) Definition() sdk.ChatCompletionTool {
	description := "Gets the currently focused (frontmost) application. Returns the application name and bundle identifier. Use this before performing computer use actions to verify the correct application is in focus."
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

	appID, err := focusManager.GetFrontmostApp(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get focused app: %w", err)
	}

	if appID == "" {
		return nil, fmt.Errorf("no application is currently focused")
	}

	// Parse app name from bundle ID (e.g., "org.mozilla.firefox" -> "Firefox")
	appName := parseAppName(appID)

	result := fmt.Sprintf("Currently focused application:\n- Name: %s\n- Bundle ID: %s", appName, appID)

	return &domain.ToolExecutionResult{
		ToolName: "GetFocusedApp",
		Success:  true,
		Data: map[string]any{
			"app_name":  appName,
			"bundle_id": appID,
			"message":   result,
		},
	}, nil
}

// IsEnabled returns whether the tool is enabled
func (t *GetFocusedAppTool) IsEnabled() bool {
	return t.config.GetConfig().ComputerUse.Enabled
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
	if appName, ok := data["app_name"].(string); ok {
		return fmt.Sprintf("Focused: %s", appName)
	}
	return "Got focused app"
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

// parseAppName extracts a human-readable app name from bundle ID
func parseAppName(bundleID string) string {
	// Common mappings
	appNames := map[string]string{
		"com.apple.Terminal":        "Terminal",
		"com.googlecode.iterm2":     "iTerm2",
		"com.microsoft.VSCode":      "Visual Studio Code",
		"org.mozilla.firefox":       "Firefox",
		"com.google.Chrome":         "Google Chrome",
		"com.apple.Safari":          "Safari",
		"com.microsoft.edgemac":     "Microsoft Edge",
		"com.brave.Browser":         "Brave Browser",
		"com.sublimetext.4":         "Sublime Text",
		"com.jetbrains.goland":      "GoLand",
		"com.jetbrains.intellij":    "IntelliJ IDEA",
		"org.alacritty":             "Alacritty",
		"net.kovidgoyal.kitty":      "Kitty",
		"com.apple.finder":          "Finder",
		"com.apple.TextEdit":        "TextEdit",
		"com.spotify.client":        "Spotify",
		"com.tinyspeck.slackmacgap": "Slack",
		"us.zoom.xos":               "Zoom",
		"com.microsoft.teams":       "Microsoft Teams",
		"com.docker.docker":         "Docker Desktop",
		"com.postmanlabs.app":       "Postman",
		"com.notion.desktop":        "Notion",
		"com.figma.Desktop":         "Figma",
		"com.apple.Notes":           "Notes",
		"com.apple.mail":            "Mail",
		"com.apple.iCal":            "Calendar",
	}

	if name, ok := appNames[bundleID]; ok {
		return name
	}

	// Fallback: return bundle ID
	return bundleID
}
