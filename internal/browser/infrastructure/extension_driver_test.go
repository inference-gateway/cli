package infrastructure

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	browserdomain "github.com/inference-gateway/cli/internal/browser/domain"
)

// driverConfig sets a 2s action timeout, so frames must carry timeout_ms 2000.
func driverConfig() *config.BrowserUseConfig {
	cfg := config.DefaultBrowserUseConfig()
	cfg.Browser.TimeoutSeconds = 2
	return cfg
}

// newCapturingDriver builds an ExtensionDriver whose injected request records
// each browser_command frame and replies with the canned result.
func newCapturingDriver(t *testing.T, replay extensionResult) (*ExtensionDriver, func() []extensionCommand) {
	t.Helper()
	var calls []extensionCommand
	request := func(_ context.Context, id string, frame json.RawMessage) (json.RawMessage, error) {
		var cmd extensionCommand
		if err := json.Unmarshal(frame, &cmd); err != nil {
			t.Errorf("bad browser_command frame: %v", err)
		}
		if cmd.ID != id || cmd.Type != frameBrowserCommand || cmd.TimeoutMs != 2000 {
			t.Errorf("frame id/type/timeout_ms mismatch: %s", frame)
		}
		calls = append(calls, cmd)
		data, err := json.Marshal(replay)
		if err != nil {
			t.Errorf("marshal replay: %v", err)
		}
		return data, err
	}
	return NewExtensionDriver(driverConfig(), request), func() []extensionCommand { return calls }
}

// checkResult compares the flat BrowserToolResult fields the verbs produce.
func checkResult(t *testing.T, got, want browserdomain.BrowserToolResult) {
	t.Helper()
	if got.Action != want.Action || got.URL != want.URL || got.Title != want.Title ||
		got.Selector != want.Selector || got.Text != want.Text || got.Content != want.Content {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
	if !slices.Equal(got.Events, want.Events) {
		t.Fatalf("events = %v, want %v", got.Events, want.Events)
	}
}

func TestExtensionDriverMapsVerbsToFramesAndResults(t *testing.T) {
	tests := []struct {
		name  string
		play  func(d *ExtensionDriver) (browserdomain.BrowserToolResult, error)
		frame extensionCommand
		want  browserdomain.BrowserToolResult
	}{
		{
			name: "navigate",
			play: func(d *ExtensionDriver) (browserdomain.BrowserToolResult, error) {
				return d.Navigate(context.Background(), "https://example.com")
			},
			frame: extensionCommand{Action: browserActionNavigate, URL: "https://example.com"},
			want:  browserdomain.BrowserToolResult{Action: browserActionNavigate, URL: "https://example.com", Title: "Example Domain"},
		},
		{
			name: "click",
			play: func(d *ExtensionDriver) (browserdomain.BrowserToolResult, error) {
				return d.Click(context.Background(), "#submit")
			},
			frame: extensionCommand{Action: browserActionClick, Selector: "#submit"},
			want:  browserdomain.BrowserToolResult{Action: browserActionClick, Selector: "#submit", URL: "https://example.com", Title: "Example Domain"},
		},
		{
			name: "type",
			play: func(d *ExtensionDriver) (browserdomain.BrowserToolResult, error) {
				return d.Type(context.Background(), "#query", "hi", true)
			},
			frame: extensionCommand{Action: browserActionType, Selector: "#query", Text: "hi", PressEnter: true},
			want:  browserdomain.BrowserToolResult{Action: browserActionType, Selector: "#query", Text: "hi", URL: "https://example.com", Title: "Example Domain"},
		},
		{
			name: "read",
			play: func(d *ExtensionDriver) (browserdomain.BrowserToolResult, error) {
				return d.Read(context.Background(), "main")
			},
			frame: extensionCommand{Action: browserActionRead, Selector: "main"},
			want: browserdomain.BrowserToolResult{
				Action:   browserActionRead,
				Selector: "main",
				URL:      "https://example.com",
				Title:    "Example Domain",
				Content:  "the page text",
				Events:   []string{"evt-1", "evt-2"},
			},
		},
	}
	replay := extensionResult{URL: "https://example.com", Title: "Example Domain", Content: "the page text", Events: []string{"evt-1", "evt-2"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver, calls := newCapturingDriver(t, replay)
			got, err := tt.play(driver)
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			checkResult(t, got, tt.want)

			sent := calls()
			if len(sent) != 1 || sent[0].Action != tt.frame.Action || sent[0].URL != tt.frame.URL ||
				sent[0].Selector != tt.frame.Selector || sent[0].Text != tt.frame.Text || sent[0].PressEnter != tt.frame.PressEnter {
				t.Fatalf("%s: unexpected frames %+v", tt.name, sent)
			}
		})
	}
}

func TestExtensionDriverScreenshotMapsImage(t *testing.T) {
	driver, _ := newCapturingDriver(t, extensionResult{Image: "iVBOR", ImageMimeType: "image/jpeg", URL: "https://example.com", Title: "Example Domain"})
	result, err := driver.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if result.Data != "iVBOR" || result.MimeType != "image/jpeg" || result.URL != "https://example.com" || result.Title != "Example Domain" {
		t.Fatalf("unexpected screenshot result: %+v", result)
	}
}

func TestExtensionDriverScreenshotDefaultsMimeToPng(t *testing.T) {
	driver, _ := newCapturingDriver(t, extensionResult{Image: "iVBOR"})
	result, err := driver.Screenshot(context.Background())
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if result.MimeType != "image/png" {
		t.Fatalf("mime type = %q, want the image/png default", result.MimeType)
	}
}

func TestExtensionDriverScreenshotWithoutDataFails(t *testing.T) {
	driver, _ := newCapturingDriver(t, extensionResult{})
	if _, err := driver.Screenshot(context.Background()); err == nil || !strings.Contains(err.Error(), "no screenshot data") {
		t.Fatalf("expected the no-data error, got %v", err)
	}
}

func TestExtensionDriverTabsReturnsTabs(t *testing.T) {
	replay := extensionResult{Tabs: []browserdomain.BrowserTab{{Index: 0, URL: "https://example.com", Title: "Example", Active: true}}}
	driver, calls := newCapturingDriver(t, replay)
	tabs, err := driver.Tabs(context.Background())
	if err != nil {
		t.Fatalf("Tabs: %v", err)
	}
	if len(tabs) != 1 || !tabs[0].Active || tabs[0].URL != "https://example.com" {
		t.Fatalf("unexpected tabs: %+v", tabs)
	}
	if sent := calls(); len(sent) != 1 || sent[0].Action != browserActionTabs {
		t.Fatalf("unexpected frames %+v", sent)
	}
}

func TestExtensionDriverSurfacesExtensionError(t *testing.T) {
	driver, _ := newCapturingDriver(t, extensionResult{Error: "no such element"})
	_, err := driver.Navigate(context.Background(), "https://example.com")
	if err == nil || !strings.Contains(err.Error(), "failed to navigate: no such element") {
		t.Fatalf("expected the action error, got %v", err)
	}
}

func TestExtensionDriverTimesOutWithActionError(t *testing.T) {
	request := func(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		return nil, context.DeadlineExceeded
	}
	driver := NewExtensionDriver(driverConfig(), request)
	if _, err := driver.Tabs(context.Background()); err == nil || !strings.Contains(err.Error(), "timed out waiting for the browser extension to tabs") {
		t.Fatalf("expected the timeout error, got %v", err)
	}
}

func TestExtensionDriverClickAtUnsupported(t *testing.T) {
	driver := NewExtensionDriver(driverConfig(), func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		return nil, nil
	})
	if _, err := driver.ClickAt(context.Background(), 10, 20); err == nil || !strings.Contains(err.Error(), "coordinate click isn't supported") {
		t.Fatalf("expected the unsupported error, got %v", err)
	}
}
