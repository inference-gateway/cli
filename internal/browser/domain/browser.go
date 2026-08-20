// Package domain holds the browser-use bounded context's contracts: the
// driver interface the tools consume and the wire/result types it produces.
// It is pure - stdlib imports only.
package domain

import "context"

// BrowserToolResult represents the result of a browser use operation. One
// shared shape for all browser tools; each tool fills the fields it produces.
type BrowserToolResult struct {
	Action   string   `json:"action"`
	URL      string   `json:"url,omitempty"`
	Title    string   `json:"title,omitempty"`
	Selector string   `json:"selector,omitempty"`
	Text     string   `json:"text,omitempty"`
	Content  string   `json:"content,omitempty"`
	Events   []string `json:"events,omitempty"`
}

// BrowserTab describes one open tab/page as reported by BrowserTabs. Active
// marks the tab the browser-use verbs currently act on.
type BrowserTab struct {
	Index  int    `json:"index"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// BrowserScreenshotResult carries a screenshot of the current page. Data is
// base64-encoded image bytes; the BrowserScreenshot tool persists them and
// sets the attachment's on-disk source path.
type BrowserScreenshotResult struct {
	Data     string `json:"-"`
	MimeType string `json:"mime_type"`
	URL      string `json:"url,omitempty"`
	Title    string `json:"title,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

// BrowserDriver executes browser-use verbs against a browser backend: a
// Playwright-launched browser, or the user's real browser via the opentask
// extension bridge.
type BrowserDriver interface {
	Navigate(ctx context.Context, url string) (BrowserToolResult, error)
	Click(ctx context.Context, selector string) (BrowserToolResult, error)
	// ClickAt clicks at viewport coordinates (CSS pixels), for use with a
	// screenshot. Not every backend supports it (the extension bridge does not).
	ClickAt(ctx context.Context, x, y float64) (BrowserToolResult, error)
	Type(ctx context.Context, selector, text string, pressEnter bool) (BrowserToolResult, error)
	// Read returns element text in Content and drained browser events in Events.
	// Sensitive input values (passwords, tokens) MUST be redacted before return.
	Read(ctx context.Context, selector string) (BrowserToolResult, error)
	// Screenshot captures the current page/active tab as an image.
	Screenshot(ctx context.Context) (BrowserScreenshotResult, error)
	// Tabs lists the open tabs, marking the active one.
	Tabs(ctx context.Context) ([]BrowserTab, error)
	Close()
}
