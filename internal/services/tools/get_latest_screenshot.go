package tools

import (
	"context"
	"fmt"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// GetLatestScreenshotTool retrieves the latest screenshot from the circular buffer
// This tool is used when screenshot streaming is enabled to avoid redundant captures
type GetLatestScreenshotTool struct {
	config          *config.Config
	enabled         bool
	formatter       domain.BaseFormatter
	provider        domain.ScreenshotProvider
	lastCallTime    time.Time
	minCallInterval time.Duration
}

// NewGetLatestScreenshotTool creates a new tool that reads from the screenshot buffer
func NewGetLatestScreenshotTool(cfg *config.Config, provider domain.ScreenshotProvider) *GetLatestScreenshotTool {
	minInterval := time.Duration(cfg.ComputerUse.Screenshot.CaptureInterval) * time.Second
	if minInterval < 2*time.Second {
		minInterval = 2 * time.Second
	}

	return &GetLatestScreenshotTool{
		config:          cfg,
		enabled:         cfg.ComputerUse.Enabled && cfg.ComputerUse.Screenshot.StreamingEnabled,
		formatter:       domain.NewBaseFormatter("GetLatestScreenshot"),
		provider:        provider,
		minCallInterval: minInterval,
	}
}

// Definition returns the tool definition for the LLM
func (t *GetLatestScreenshotTool) Definition() sdk.ChatCompletionTool {
	description := "Retrieves the latest screenshot from the buffer. This is a read-only operation that does NOT require approval. Use this tool to see the current state of the screen. Screenshots are automatically captured every few seconds when streaming is enabled."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "GetLatestScreenshot",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

// Execute retrieves the latest screenshot from the buffer
func (t *GetLatestScreenshotTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.lastCallTime.IsZero() {
		timeSinceLastCall := time.Since(t.lastCallTime)
		if timeSinceLastCall < t.minCallInterval {
			waitTime := t.minCallInterval - timeSinceLastCall
			return &domain.ToolExecutionResult{
				ToolName:  "GetLatestScreenshot",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     fmt.Sprintf("please wait %v before requesting another screenshot (last called %v ago)", waitTime.Round(time.Second), timeSinceLastCall.Round(time.Second)),
			}, nil
		}
	}

	t.lastCallTime = time.Now()

	if t.provider == nil {
		return &domain.ToolExecutionResult{
			ToolName:  "GetLatestScreenshot",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "screenshot provider not available",
		}, nil
	}

	screenshot, err := t.provider.GetLatestScreenshot()
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "GetLatestScreenshot",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	mimeType := "image/" + screenshot.Format
	imageAttachment := domain.ImageAttachment{
		Data:        screenshot.Data,
		MimeType:    mimeType,
		DisplayName: "screenshot-latest",
	}

	result := domain.ScreenshotToolResult{
		Region: nil,
		Width:  screenshot.Width,
		Height: screenshot.Height,
		Format: screenshot.Format,
		Method: screenshot.Method,
	}

	return &domain.ToolExecutionResult{
		ToolName:  "GetLatestScreenshot",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
		Images:    []domain.ImageAttachment{imageAttachment},
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *GetLatestScreenshotTool) Validate(args map[string]any) error {
	// No arguments needed
	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *GetLatestScreenshotTool) IsEnabled() bool {
	return t.enabled && t.provider != nil
}

// FormatResult formats tool execution results for different contexts
func (t *GetLatestScreenshotTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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
func (t *GetLatestScreenshotTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to get latest screenshot"
	}
	data, ok := result.Data.(domain.ScreenshotToolResult)
	if !ok {
		return "Latest screenshot retrieved"
	}
	return fmt.Sprintf("Latest screenshot: %dx%d (%s)", data.Width, data.Height, data.Method)
}

// FormatForLLM formats the result for LLM consumption
func (t *GetLatestScreenshotTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(domain.ScreenshotToolResult)
	if !ok {
		return "Latest screenshot retrieved successfully. Image is attached."
	}
	return fmt.Sprintf("Latest screenshot retrieved successfully (%dx%d, format: %s, method: %s). This screenshot was automatically captured by the streaming system. Image is attached.",
		data.Width, data.Height, data.Format, data.Method)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *GetLatestScreenshotTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *GetLatestScreenshotTool) ShouldAlwaysExpand() bool {
	return false
}
