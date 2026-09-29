package infrastructure

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	uuid "github.com/google/uuid"

	config "github.com/inference-gateway/cli/config"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
)

// Frame and action names of the browser_command wire contract, as documented in
// docs/browser-extension-protocol.md.
const (
	outboundBrowserCommand = "browser_command"

	browserActionNavigate   = "navigate"
	browserActionClick      = "click"
	browserActionType       = "type"
	browserActionRead       = "read"
	browserActionScreenshot = "screenshot"
	browserActionTabs       = "tabs"
)

// defaultActionTimeoutSeconds applies when browser_use.browser.timeout_seconds is unset.
const defaultActionTimeoutSeconds = 30

const defaultScreenshotMimeType = "image/png"

// ExtensionRequest sends one browser_command frame and waits for the
// browser_result carrying id. The container injects the request, so this
// adapter touches no protocol package.
type ExtensionRequest func(ctx context.Context, id string, frame json.RawMessage) (json.RawMessage, error)

// extensionCommand is the browser_command frame the extension understands.
type extensionCommand struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	Action     string `json:"action"`
	URL        string `json:"url,omitempty"`
	Selector   string `json:"selector,omitempty"`
	Text       string `json:"text,omitempty"`
	PressEnter bool   `json:"press_enter,omitempty"`
	TimeoutMs  int    `json:"timeout_ms"`
}

// extensionResult is the browser_result payload mapped to BrowserToolResult.
type extensionResult struct {
	Error         string                     `json:"error,omitempty"`
	URL           string                     `json:"url,omitempty"`
	Title         string                     `json:"title,omitempty"`
	Content       string                     `json:"content,omitempty"`
	Events        []string                   `json:"events,omitempty"`
	Image         string                     `json:"image,omitempty"`
	ImageMimeType string                     `json:"image_mime_type,omitempty"`
	Tabs          []browserdomain.BrowserTab `json:"tabs,omitempty"`
}

// ExtensionDriver implements browserdomain.BrowserDriver against the opentask
// extension. It builds browser_command frames, owns the per-action timeout and
// maps results to BrowserToolResult, leaving the socket itself behind the
// injected ExtensionRequest.
type ExtensionDriver struct {
	request    ExtensionRequest
	actionWait time.Duration
}

// NewExtensionDriver builds the extension-backed driver from the browser_use
// configuration and the injected request seam.
func NewExtensionDriver(cfg *config.BrowserUseConfig, request ExtensionRequest) *ExtensionDriver {
	actionWait := defaultActionTimeoutSeconds * time.Second
	if cfg.Browser.TimeoutSeconds > 0 {
		actionWait = time.Duration(cfg.Browser.TimeoutSeconds) * time.Second
	}
	return &ExtensionDriver{request: request, actionWait: actionWait}
}

// Navigate implements browserdomain.BrowserDriver.
func (d *ExtensionDriver) Navigate(ctx context.Context, url string) (browserdomain.BrowserToolResult, error) {
	result, err := d.send(ctx, extensionCommand{Action: browserActionNavigate, URL: url})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{Action: browserActionNavigate, URL: result.URL, Title: result.Title}, nil
}

// Click implements browserdomain.BrowserDriver.
func (d *ExtensionDriver) Click(ctx context.Context, selector string) (browserdomain.BrowserToolResult, error) {
	result, err := d.send(ctx, extensionCommand{Action: browserActionClick, Selector: selector})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{Action: browserActionClick, Selector: selector, URL: result.URL, Title: result.Title}, nil
}

// Type implements browserdomain.BrowserDriver.
func (d *ExtensionDriver) Type(ctx context.Context, selector, text string, pressEnter bool) (browserdomain.BrowserToolResult, error) {
	result, err := d.send(ctx, extensionCommand{Action: browserActionType, Selector: selector, Text: text, PressEnter: pressEnter})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{Action: browserActionType, Selector: selector, Text: text, URL: result.URL, Title: result.Title}, nil
}

// Read implements browserdomain.BrowserDriver.
func (d *ExtensionDriver) Read(ctx context.Context, selector string) (browserdomain.BrowserToolResult, error) {
	result, err := d.send(ctx, extensionCommand{Action: browserActionRead, Selector: selector})
	if err != nil {
		return browserdomain.BrowserToolResult{}, err
	}
	return browserdomain.BrowserToolResult{
		Action:   browserActionRead,
		Selector: selector,
		URL:      result.URL,
		Title:    result.Title,
		Content:  result.Content,
		Events:   result.Events,
	}, nil
}

// ClickAt implements browserdomain.BrowserDriver. The extension drives clicks
// through chrome.scripting (untrusted synthetic events), which have no reliable
// viewport-coordinate form - that needs chrome.debugger/CDP. Fail clearly.
func (d *ExtensionDriver) ClickAt(_ context.Context, _, _ float64) (browserdomain.BrowserToolResult, error) {
	return browserdomain.BrowserToolResult{}, errors.New("coordinate click isn't supported on the extension backend; use a CSS or text= selector with BrowserClick")
}

// Screenshot implements browserdomain.BrowserDriver via the extension's captureVisibleTab.
func (d *ExtensionDriver) Screenshot(ctx context.Context) (browserdomain.BrowserScreenshotResult, error) {
	result, err := d.send(ctx, extensionCommand{Action: browserActionScreenshot})
	if err != nil {
		return browserdomain.BrowserScreenshotResult{}, err
	}
	if result.Image == "" {
		return browserdomain.BrowserScreenshotResult{}, errors.New("extension returned no screenshot data")
	}
	return browserdomain.BrowserScreenshotResult{
		Data:     result.Image,
		MimeType: cmp.Or(result.ImageMimeType, defaultScreenshotMimeType),
		URL:      result.URL,
		Title:    result.Title,
	}, nil
}

// Tabs implements browserdomain.BrowserDriver via the extension's chrome.tabs query.
func (d *ExtensionDriver) Tabs(ctx context.Context) ([]browserdomain.BrowserTab, error) {
	result, err := d.send(ctx, extensionCommand{Action: browserActionTabs})
	if err != nil {
		return nil, err
	}
	return result.Tabs, nil
}

// Close implements browserdomain.BrowserDriver. The container closes the
// bridge, and the driver owns nothing to shut down.
func (d *ExtensionDriver) Close() {}

// send dispatches one browser command and waits for its result. The deadline
// gives the extension its own in-frame timeout plus a reply margin.
func (d *ExtensionDriver) send(ctx context.Context, cmd extensionCommand) (*extensionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, d.actionWait+5*time.Second)
	defer cancel()

	cmd.Type = outboundBrowserCommand
	cmd.ID = uuid.NewString()
	cmd.TimeoutMs = int(d.actionWait / time.Millisecond)
	frame, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	raw, err := d.request(ctx, cmd.ID, frame)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("timed out waiting for the browser extension to %s - is the opentask extension still running?", cmd.Action)
		}
		return nil, err
	}

	var result extensionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decoding the browser_result for %s: %w", cmd.Action, err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("failed to %s: %s", cmd.Action, result.Error)
	}
	return &result, nil
}
