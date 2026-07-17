package tools

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestFetchTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WebFetch: config.WebFetchToolConfig{
				Enabled: true,
			},
		},
		Prompts: *config.DefaultPromptsConfig(),
	}

	tool := NewWebFetchTool(cfg)
	def := tool.Definition()

	if def.Function.Name != "WebFetch" {
		t.Errorf("Expected tool name 'WebFetch', got %s", def.Function.Name)
	}

	if *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Function.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestFetchTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name          string
		toolsEnabled  bool
		fetchEnabled  bool
		expectedState bool
	}{
		{
			name:          "enabled when both tools and fetch enabled",
			toolsEnabled:  true,
			fetchEnabled:  true,
			expectedState: true,
		},
		{
			name:          "disabled when tools disabled",
			toolsEnabled:  false,
			fetchEnabled:  true,
			expectedState: false,
		},
		{
			name:          "disabled when fetch disabled",
			toolsEnabled:  true,
			fetchEnabled:  false,
			expectedState: false,
		},
		{
			name:          "disabled when both disabled",
			toolsEnabled:  false,
			fetchEnabled:  false,
			expectedState: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
					WebFetch: config.WebFetchToolConfig{
						Enabled: tt.fetchEnabled,
					},
				},
			}

			tool := NewWebFetchTool(cfg)
			if tool.IsEnabled() != tt.expectedState {
				t.Errorf("Expected IsEnabled() = %v, got %v", tt.expectedState, tool.IsEnabled())
			}
		})
	}
}

func TestFetchTool_Validate(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WebFetch: config.WebFetchToolConfig{
				Enabled: true,
				AllowedDomains: []string{
					"api.github.com",
					"httpbin.org",
					"github.com",
				},
			},
		},
	}

	tool := NewWebFetchTool(cfg)

	tests := []struct {
		name      string
		args      map[string]any
		wantError bool
	}{
		{
			name: "valid allowed URL",
			args: map[string]any{
				"url": "https://httpbin.org/json",
			},
			wantError: false,
		},
		{
			name: "valid URL with format",
			args: map[string]any{
				"url":    "https://httpbin.org/json",
				"format": "json",
			},
			wantError: false,
		},
		{
			name: "valid pattern URL",
			args: map[string]any{
				"url": "https://github.com/owner/repo/issues/123",
			},
			wantError: false,
		},
		{
			name:      "missing URL",
			args:      map[string]any{},
			wantError: true,
		},
		{
			name: "empty URL",
			args: map[string]any{
				"url": "",
			},
			wantError: true,
		},
		{
			name: "URL wrong type",
			args: map[string]any{
				"url": 123,
			},
			wantError: true,
		},
		{
			name: "disallowed URL",
			args: map[string]any{
				"url": "https://example.com/test",
			},
			wantError: true,
		},
		{
			name: "invalid format",
			args: map[string]any{
				"url":    "https://httpbin.org/json",
				"format": "xml",
			},
			wantError: true,
		},
		{
			name: "format wrong type",
			args: map[string]any{
				"url":    "https://httpbin.org/json",
				"format": 123,
			},
			wantError: true,
		},
		{
			name: "file:// protocol not allowed",
			args: map[string]any{
				"url": "file:///etc/passwd",
			},
			wantError: true,
		},
		{
			name: "ftp:// protocol not allowed",
			args: map[string]any{
				"url": "ftp://example.com/file.txt",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestFetchTool_Execute_Disabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
			WebFetch: config.WebFetchToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWebFetchTool(cfg)
	ctx := context.Background()

	args := map[string]any{
		"url": "https://httpbin.org/json",
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when tool is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when tool is disabled")
	}
}

func TestFetchTool_Execute_FetchDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WebFetch: config.WebFetchToolConfig{
				Enabled: false,
			},
		},
	}

	tool := NewWebFetchTool(cfg)
	ctx := context.Background()

	args := map[string]any{
		"url": "https://httpbin.org/json",
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when fetch is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when fetch is disabled")
	}
}

// newHTTPTestFetchTool builds a WebFetch tool that allows loopback URLs and
// saves downloads under a per-test temp dir.
func newHTTPTestFetchTool(t *testing.T) *WebFetchTool {
	t.Helper()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WebFetch: config.WebFetchToolConfig{
				Enabled:        true,
				AllowedDomains: []string{"127.0.0.1"},
				Safety:         config.FetchSafetyConfig{MaxSize: 10 * 1024 * 1024, Timeout: 30},
			},
		},
	}
	cfg.SetConfigDir(t.TempDir())
	return NewWebFetchTool(cfg)
}

// TestFetchTool_Execute_BinarySavedNotInlined proves binary content (a PNG) is
// saved to disk byte-for-byte but never inlined into the LLM context — the bug
// that ballooned a Telegram run past the model's context window.
func TestFetchTool_Execute_BinarySavedNotInlined(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02, 0xFF, 0xFE}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	tool := newHTTPTestFetchTool(t)
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	fr, ok := result.Data.(*domain.FetchResult)
	if !ok {
		t.Fatalf("expected *domain.FetchResult, got %T", result.Data)
	}
	if strings.Contains(fr.Content, string(pngBytes)) {
		t.Error("raw binary bytes leaked into Content (would poison the LLM context)")
	}
	if !strings.Contains(fr.Content, "saved to disk") {
		t.Errorf("expected a saved-to-disk marker in Content, got: %q", fr.Content)
	}

	if fr.SavedPath == "" {
		t.Fatal("expected SavedPath to be set for binary content")
	}
	onDisk, readErr := os.ReadFile(fr.SavedPath)
	if readErr != nil {
		t.Fatalf("reading saved file: %v", readErr)
	}
	if !bytes.Equal(onDisk, pngBytes) {
		t.Errorf("saved file corrupted: got %v, want %v", onDisk, pngBytes)
	}

	if llm := tool.FormatResult(result, domain.FormatterLLM); strings.Contains(llm, string(pngBytes)) {
		t.Error("raw binary bytes leaked into FormatResult(FormatterLLM)")
	}
}

// TestFetchTool_Execute_TextStillInlined is the regression guard: textual
// content is inlined as before and not auto-saved.
func TestFetchTool_Execute_TextStillInlined(t *testing.T) {
	const page = "<html><body>hello world</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	tool := newHTTPTestFetchTool(t)
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	fr, ok := result.Data.(*domain.FetchResult)
	if !ok {
		t.Fatalf("expected *domain.FetchResult, got %T", result.Data)
	}
	if !strings.Contains(fr.Content, "hello world") {
		t.Errorf("text content should be inlined, got: %q", fr.Content)
	}
	if fr.SavedPath != "" {
		t.Errorf("text content should not be auto-saved, got SavedPath=%q", fr.SavedPath)
	}
}

// TestFetchTool_Execute_ChannelImageDisplayHint proves that inside a Telegram
// channel session a fetched image gets a display hint — ![image](path) — instead
// of the plain marker, so the channel's existing auto-uploader sends it as a
// photo, while the raw bytes still never reach the LLM.
func TestFetchTool_Execute_ChannelImageDisplayHint(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02, 0xFF, 0xFE}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	tool := newHTTPTestFetchTool(t)
	ctx := domain.WithSessionID(context.Background(), "channel-telegram-12345")
	result, err := tool.Execute(ctx, map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	fr, ok := result.Data.(*domain.FetchResult)
	if !ok {
		t.Fatalf("expected *domain.FetchResult, got %T", result.Data)
	}
	if fr.SavedPath == "" {
		t.Fatal("expected SavedPath to be set")
	}
	if !strings.Contains(fr.Content, "![image]("+fr.SavedPath+")") {
		t.Errorf("expected a display hint referencing the saved path, got: %q", fr.Content)
	}
	if strings.Contains(fr.Content, string(pngBytes)) {
		t.Error("raw binary bytes leaked into Content")
	}
}
