package browser

import (
	"context"
	"encoding/base64"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
	"os"
	"path/filepath"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
)

// BrowserScreenshotTool captures the current browser page as an image and
// attaches it to the conversation (vision models see it inline; others get a
// file path for ImageDecode).
type BrowserScreenshotTool struct {
	browserToolBase
	config *config.Config
}

// NewBrowserScreenshotTool creates a new browser screenshot tool
func NewBrowserScreenshotTool(cfg *config.Config, rateLimiter rateLimiter, driver browserdomain.BrowserDriver) *BrowserScreenshotTool {
	return &BrowserScreenshotTool{
		browserToolBase: browserToolBase{
			name:        "BrowserScreenshot",
			enabled:     cfg.BrowserUse.Enabled && cfg.BrowserUse.Tools.Screenshot.Enabled,
			driver:      driver,
			rateLimiter: rateLimiter,
		},
		config: cfg,
	}
}

// Definition returns the tool definition for the LLM
func (t *BrowserScreenshotTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.BrowserScreenshot.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "BrowserScreenshot",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []string{},
			},
		},
	}
}

// Execute captures the screenshot and returns it as an attached image
func (t *BrowserScreenshotTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.checkRateLimit(); err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	res, err := t.driver.Screenshot(ctx)
	if err != nil {
		return t.errorResult(args, start, err.Error()), nil
	}

	var path string
	if raw, derr := base64.StdEncoding.DecodeString(res.Data); derr == nil {
		path, _ = t.persistScreenshot(raw)
	}

	attachment := agentdomain.ImageAttachment{
		Data:        res.Data,
		MimeType:    res.MimeType,
		DisplayName: "browser-screenshot",
		SourcePath:  path,
	}
	return &agentdomain.ToolExecutionResult{
		ToolName:  t.name,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      browserdomain.BrowserToolResult{Action: "screenshot", URL: res.URL, Title: res.Title},
		Images:    []agentdomain.ImageAttachment{attachment},
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *BrowserScreenshotTool) Validate(map[string]any) error {
	return nil
}

// persistScreenshot writes the PNG to <configdir>/tmp/screenshots, the same
// retention-managed scratch dir the computer-use screenshot server uses and
// which is carved out of the tool sandbox so ImageDecode can read it back.
func (t *BrowserScreenshotTool) persistScreenshot(data []byte) (string, error) {
	dir := filepath.Join(t.config.GetConfigDir(), "tmp", "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "browser-*.png")
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	path := f.Name()
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path, nil
}
