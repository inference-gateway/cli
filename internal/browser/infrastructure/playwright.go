package infrastructure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	playwright "github.com/mxschmitt/playwright-go"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/browser/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// maxBrowserEvents caps the browser-initiated event buffer; older events are
// dropped once the cap is reached.
const maxBrowserEvents = 50

// maxBrowserReadChars bounds how much page text a single BrowserRead returns
// so one read cannot flood the conversation context.
const maxBrowserReadChars = 50000

// browserSession is the single shared browser the browser use tools drive via
// Playwright (CDP under the hood for Chromium). It launches lazily on first
// use - either the user's installed browser (config channel, e.g. "chrome"),
// an already-running browser via a CDP endpoint, or Playwright's bundled
// Chromium as a fallback - and stays open across tool calls so navigation
// state persists.
//
// Browser-initiated actions reach the CLI through the event buffer: console
// messages, dialogs (auto-dismissed), and page scripts calling
// window.inferNotify(...) are recorded and surfaced by the BrowserRead tool.
type browserSession struct {
	cfg *config.BrowserUseConfig

	mu      sync.Mutex
	pw      *playwright.Playwright
	browser playwright.Browser
	page    playwright.Page

	eventsMu sync.Mutex
	events   []string
}

func NewSession(cfg *config.BrowserUseConfig) *browserSession {
	return &browserSession{cfg: cfg}
}

// timeoutMs returns the configured per-action timeout in milliseconds.
func (s *browserSession) timeoutMs() float64 {
	secs := s.cfg.Browser.TimeoutSeconds
	if secs <= 0 {
		secs = 30
	}
	return float64(secs) * 1000
}

// Page returns the active page, launching the browser on first use.
func (s *browserSession) Page() (playwright.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.page != nil && !s.page.IsClosed() {
		return s.page, nil
	}

	if err := s.ensureBrowser(); err != nil {
		return nil, err
	}

	page, err := s.newPage()
	if err != nil {
		return nil, fmt.Errorf("failed to open page: %w", err)
	}

	s.attachPageListeners(page)
	s.page = page
	return page, nil
}

// ensureBrowser starts the Playwright driver and connects or launches the
// browser. Callers must hold s.mu.
//
//nolint:nestif
func (s *browserSession) ensureBrowser() error {
	if s.browser != nil && s.browser.IsConnected() {
		return nil
	}

	if s.pw == nil {
		pw, err := playwright.Run()
		if err != nil {
			// Driver not installed yet - fetch it (browsers are skipped; we
			// prefer the user's own browser via the configured channel).
			if instErr := playwright.Install(&playwright.RunOptions{SkipInstallBrowsers: true}); instErr != nil {
				return fmt.Errorf("failed to install playwright driver: %w", instErr)
			}
			pw, err = playwright.Run()
			if err != nil {
				return fmt.Errorf("failed to start playwright: %w", err)
			}
		}
		s.pw = pw
	}

	if endpoint := s.cfg.Browser.CDPEndpoint; endpoint != "" {
		browser, err := s.pw.Chromium.ConnectOverCDP(endpoint)
		if err != nil {
			return fmt.Errorf("failed to connect to browser over CDP at %s: %w", endpoint, err)
		}
		s.browser = browser
		return nil
	}

	opts := playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(s.cfg.Browser.Headless),
	}
	if s.cfg.Browser.Channel != "" {
		opts.Channel = playwright.String(s.cfg.Browser.Channel)
	}

	browser, err := s.pw.Chromium.Launch(opts)
	if err != nil && s.cfg.Browser.Channel != "" {
		// The configured browser is not installed - fall back to Playwright's
		// bundled Chromium (downloading it if needed).
		logger.Warn("failed to launch configured browser channel, falling back to bundled chromium",
			"channel", s.cfg.Browser.Channel, "error", err)
		if instErr := playwright.Install(); instErr != nil {
			return fmt.Errorf("failed to install fallback chromium: %w", instErr)
		}
		opts.Channel = nil
		browser, err = s.pw.Chromium.Launch(opts)
	}
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}
	s.browser = browser
	return nil
}

// newPage reuses an existing page when attached over CDP (so we drive what
// the user already has open) and opens a fresh one otherwise. Callers must
// hold s.mu.
func (s *browserSession) newPage() (playwright.Page, error) {
	if s.cfg.Browser.CDPEndpoint != "" {
		for _, ctx := range s.browser.Contexts() {
			if pages := ctx.Pages(); len(pages) > 0 {
				return pages[0], nil
			}
		}
	}
	return s.browser.NewPage()
}

// attachPageListeners wires browser-to-CLI channels: console messages,
// dialogs (auto-dismissed so automation never hangs on an alert), and a
// window.inferNotify(...) binding page scripts can call to signal the CLI.
func (s *browserSession) attachPageListeners(page playwright.Page) {
	page.OnConsole(func(msg playwright.ConsoleMessage) {
		s.recordEvent(fmt.Sprintf("console.%s: %s", msg.Type(), msg.Text()))
	})
	page.OnDialog(func(dialog playwright.Dialog) {
		s.recordEvent(fmt.Sprintf("dialog.%s (dismissed): %s", dialog.Type(), dialog.Message()))
		if err := dialog.Dismiss(); err != nil {
			logger.Warn("failed to dismiss browser dialog", "error", err)
		}
	})
	if err := page.ExposeFunction("inferNotify", func(args ...any) any {
		parts := make([]string, 0, len(args))
		for _, a := range args {
			parts = append(parts, fmt.Sprintf("%v", a))
		}
		s.recordEvent("inferNotify: " + strings.Join(parts, " "))
		return nil
	}); err != nil {
		logger.Warn("failed to expose inferNotify binding", "error", err)
	}
}

// recordEvent appends a browser-initiated event, dropping the oldest beyond
// the cap. Safe for concurrent use - playwright dispatches events on their
// own goroutines.
func (s *browserSession) recordEvent(event string) {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	s.events = append(s.events, event)
	if len(s.events) > maxBrowserEvents {
		s.events = s.events[len(s.events)-maxBrowserEvents:]
	}
}

// DrainEvents returns and clears the buffered browser-initiated events.
func (s *browserSession) DrainEvents() []string {
	s.eventsMu.Lock()
	defer s.eventsMu.Unlock()
	events := s.events
	s.events = nil
	return events
}

// Navigate implements domain.BrowserDriver.
func (s *browserSession) Navigate(_ context.Context, url string) (domain.BrowserToolResult, error) {
	page, err := s.Page()
	if err != nil {
		return domain.BrowserToolResult{}, err
	}
	if _, err := page.Goto(url, playwright.PageGotoOptions{
		Timeout: playwright.Float(s.timeoutMs()),
	}); err != nil {
		return domain.BrowserToolResult{}, fmt.Errorf("failed to navigate to %s: %w", url, err)
	}
	title, _ := page.Title()
	return domain.BrowserToolResult{Action: "navigate", URL: page.URL(), Title: title}, nil
}

// Click implements domain.BrowserDriver.
func (s *browserSession) Click(_ context.Context, selector string) (domain.BrowserToolResult, error) {
	page, err := s.Page()
	if err != nil {
		return domain.BrowserToolResult{}, err
	}
	if err := page.Locator(selector).First().Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(s.timeoutMs()),
	}); err != nil {
		return domain.BrowserToolResult{}, fmt.Errorf("failed to click %q: %w", selector, err)
	}
	title, _ := page.Title()
	return domain.BrowserToolResult{Action: "click", Selector: selector, URL: page.URL(), Title: title}, nil
}

// Type implements domain.BrowserDriver.
func (s *browserSession) Type(_ context.Context, selector, text string, pressEnter bool) (domain.BrowserToolResult, error) {
	page, err := s.Page()
	if err != nil {
		return domain.BrowserToolResult{}, err
	}
	locator := page.Locator(selector).First()
	if err := locator.Fill(text, playwright.LocatorFillOptions{
		Timeout: playwright.Float(s.timeoutMs()),
	}); err != nil {
		return domain.BrowserToolResult{}, fmt.Errorf("failed to type into %q: %w", selector, err)
	}
	if pressEnter {
		if err := locator.Press("Enter", playwright.LocatorPressOptions{
			Timeout: playwright.Float(s.timeoutMs()),
		}); err != nil {
			return domain.BrowserToolResult{}, fmt.Errorf("failed to press Enter in %q: %w", selector, err)
		}
	}
	title, _ := page.Title()
	return domain.BrowserToolResult{Action: "type", Selector: selector, Text: text, URL: page.URL(), Title: title}, nil
}

// browserReadJS extracts text from the matched element. For form fields it
// returns the field's metadata + value (so BrowserRead can report input
// contents); for everything else it returns rendered innerText, which never
// includes input values. Go decides redaction from the metadata (see
// extractReadContent) so the security rule lives in testable Go, not JS.
const browserReadJS = `(el) => {
  const tag = el.tagName ? el.tagName.toLowerCase() : "";
  if (tag === "input" || tag === "textarea" || tag === "select") {
    return JSON.stringify({
      field: true,
      type: el.getAttribute("type") || "",
      autocomplete: el.getAttribute("autocomplete") || "",
      name: el.getAttribute("name") || "",
      id: el.id || "",
      aria: el.getAttribute("aria-label") || "",
      value: el.value || ""
    });
  }
  return JSON.stringify({ field: false, text: el.innerText || el.textContent || "" });
}`

// sensitiveNameRE flags inputs whose name/id/aria-label suggests a secret.
var sensitiveNameRE = regexp.MustCompile(`(?i)pass|secret|token|otp|cvc|card`)

// isSensitiveField decides whether an input's value must be withheld from the
// LLM. Pure so it can be unit-tested without a browser.
func isSensitiveField(fieldType, autocomplete, name, id, aria string) bool {
	if strings.EqualFold(fieldType, "password") {
		return true
	}
	switch strings.ToLower(autocomplete) {
	case "current-password", "new-password", "one-time-code":
		return true
	}
	return sensitiveNameRE.MatchString(name + " " + id + " " + aria)
}

// extractReadContent turns the browserReadJS payload into the text BrowserRead
// returns, redacting sensitive field values to "[redacted]".
func extractReadContent(raw any) string {
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	var r struct {
		Field                                           bool
		Type, Autocomplete, Name, ID, Aria, Value, Text string
	}
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return ""
	}
	if r.Field {
		if isSensitiveField(r.Type, r.Autocomplete, r.Name, r.ID, r.Aria) {
			return "[redacted]"
		}
		return r.Value
	}
	return r.Text
}

// Read implements domain.BrowserDriver. It returns rendered text (never raw
// input values), with sensitive field values redacted.
func (s *browserSession) Read(_ context.Context, selector string) (domain.BrowserToolResult, error) {
	page, err := s.Page()
	if err != nil {
		return domain.BrowserToolResult{}, err
	}
	target := selector
	if target == "" {
		target = "body"
	}
	raw, err := page.Locator(target).First().Evaluate(browserReadJS, nil)
	if err != nil {
		return domain.BrowserToolResult{}, fmt.Errorf("failed to read %q: %w", target, err)
	}
	content := extractReadContent(raw)
	if len(content) > maxBrowserReadChars {
		content = content[:maxBrowserReadChars] + "\n... (truncated)"
	}
	title, _ := page.Title()
	return domain.BrowserToolResult{
		Action:   "read",
		Selector: selector,
		URL:      page.URL(),
		Title:    title,
		Content:  content,
		Events:   s.DrainEvents(),
	}, nil
}

// ClickAt implements domain.BrowserDriver: clicks viewport coordinates (CSS
// pixels), for use with a BrowserScreenshot. ponytail: coords are CSS pixels;
// if the screenshot's devicePixelRatio != 1 the model must scale - the DPR
// knob lives in the browser context, not here.
func (s *browserSession) ClickAt(_ context.Context, x, y float64) (domain.BrowserToolResult, error) {
	page, err := s.Page()
	if err != nil {
		return domain.BrowserToolResult{}, err
	}
	if err := page.Mouse().Click(x, y); err != nil {
		return domain.BrowserToolResult{}, fmt.Errorf("failed to click at (%.0f, %.0f): %w", x, y, err)
	}
	title, _ := page.Title()
	return domain.BrowserToolResult{Action: "click", URL: page.URL(), Title: title}, nil
}

// Screenshot implements domain.BrowserDriver: captures the active page as PNG.
func (s *browserSession) Screenshot(_ context.Context) (domain.BrowserScreenshotResult, error) {
	page, err := s.Page()
	if err != nil {
		return domain.BrowserScreenshotResult{}, err
	}
	buf, err := page.Screenshot(playwright.PageScreenshotOptions{
		Timeout: playwright.Float(s.timeoutMs()),
	})
	if err != nil {
		return domain.BrowserScreenshotResult{}, fmt.Errorf("failed to screenshot page: %w", err)
	}
	title, _ := page.Title()
	res := domain.BrowserScreenshotResult{
		Data:     base64.StdEncoding.EncodeToString(buf),
		MimeType: "image/png",
		URL:      page.URL(),
		Title:    title,
	}
	if vp := page.ViewportSize(); vp != nil {
		res.Width = vp.Width
		res.Height = vp.Height
	}
	return res, nil
}

// Tabs implements domain.BrowserDriver: lists open pages across all contexts,
// marking the one the verbs currently act on.
func (s *browserSession) Tabs(_ context.Context) ([]domain.BrowserTab, error) {
	if _, err := s.Page(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	browser, active := s.browser, s.page
	s.mu.Unlock()
	if browser == nil {
		return nil, fmt.Errorf("browser not started")
	}
	var tabs []domain.BrowserTab
	idx := 0
	for _, bctx := range browser.Contexts() {
		for _, p := range bctx.Pages() {
			title, _ := p.Title()
			tabs = append(tabs, domain.BrowserTab{Index: idx, URL: p.URL(), Title: title, Active: p == active})
			idx++
		}
	}
	return tabs, nil
}

// Close shuts the browser and the Playwright driver down. Safe to call when
// nothing was ever launched.
func (s *browserSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.browser != nil {
		if err := s.browser.Close(); err != nil {
			logger.Warn("failed to close browser", "error", err)
		}
		s.browser = nil
		s.page = nil
	}
	if s.pw != nil {
		if err := s.pw.Stop(); err != nil {
			logger.Warn("failed to stop playwright", "error", err)
		}
		s.pw = nil
	}
}
