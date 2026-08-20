package browser

import (
	"context"
	"os"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// TestBrowserSessionLive exercises the real Playwright driver end-to-end against
// headless bundled Chromium. Skipped by default (it launches / may download a
// browser); run it with INFER_BROWSER_LIVE=1 to verify the screenshot, tabs,
// coordinate-click, and password-redacting read paths for real.
func TestBrowserSessionLive(t *testing.T) {
	if os.Getenv("INFER_BROWSER_LIVE") != "1" {
		t.Skip("set INFER_BROWSER_LIVE=1 to run the live browser test")
	}

	cfg := config.DefaultBrowserUseConfig()
	cfg.Browser.Headless = true
	cfg.Browser.Channel = ""
	session := NewSession(cfg)
	defer session.Close()

	ctx := context.Background()
	const page = `data:text/html,<html><head><title>login</title></head>` +
		`<body onclick="document.title='clicked'">` +
		`<p>Welcome to the site</p>` +
		`<input id="pw" type="password" name="password" value="hunter2">` +
		`<input id="q" type="text" name="q" value="searchterm">` +
		`</body></html>`

	if _, err := session.Navigate(ctx, page); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	body, err := session.Read(ctx, "")
	if err != nil {
		t.Fatalf("Read body: %v", err)
	}
	if !strings.Contains(body.Content, "Welcome to the site") {
		t.Errorf("body read missing page text: %q", body.Content)
	}
	if strings.Contains(body.Content, "hunter2") || strings.Contains(body.Content, "searchterm") {
		t.Errorf("body read leaked an input value: %q", body.Content)
	}

	pw, err := session.Read(ctx, "#pw")
	if err != nil {
		t.Fatalf("Read #pw: %v", err)
	}
	if pw.Content != "[redacted]" {
		t.Errorf("password read = %q, want [redacted]", pw.Content)
	}

	q, err := session.Read(ctx, "#q")
	if err != nil {
		t.Fatalf("Read #q: %v", err)
	}
	if q.Content != "searchterm" {
		t.Errorf("text read = %q, want searchterm", q.Content)
	}

	shot, err := session.Screenshot(ctx)
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if shot.Data == "" || shot.MimeType != "image/png" || shot.Width == 0 || shot.Height == 0 {
		t.Errorf("bad screenshot: mime=%q w=%d h=%d datalen=%d", shot.MimeType, shot.Width, shot.Height, len(shot.Data))
	}

	tabs, err := session.Tabs(ctx)
	if err != nil {
		t.Fatalf("Tabs: %v", err)
	}
	if len(tabs) == 0 || !tabs[0].Active {
		t.Errorf("tabs = %+v, want at least one active tab", tabs)
	}

	clicked, err := session.ClickAt(ctx, 30, 30)
	if err != nil {
		t.Fatalf("ClickAt: %v", err)
	}
	if clicked.Title != "clicked" {
		t.Errorf("coordinate click did not fire: title = %q", clicked.Title)
	}
}
