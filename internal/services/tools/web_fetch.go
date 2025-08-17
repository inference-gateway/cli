package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// WebFetchTool handles content fetching operations
type WebFetchTool struct {
	config  *config.Config
	enabled bool
	client  *http.Client
}

// NewWebFetchTool creates a new fetch tool
func NewWebFetchTool(cfg *config.Config) *WebFetchTool {
	return &WebFetchTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.WebFetch.Enabled,
		client: &http.Client{
			Timeout: time.Duration(cfg.Tools.WebFetch.Safety.Timeout) * time.Second,
		},
	}
}

// Definition returns the tool definition for the LLM
func (t *WebFetchTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "WebFetch",
		Description: "Fetch content from whitelisted URLs references.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "The URL to fetch content from",
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Output format (text or json)",
					"enum":        []string{"text", "json"},
					"default":     "text",
				},
			},
			"required": []string{"url"},
		},
	}
}

// Execute runs the fetch tool with given arguments
func (t *WebFetchTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled || !t.config.Tools.WebFetch.Enabled {
		return nil, fmt.Errorf("fetch tool is not enabled")
	}

	url, ok := args["url"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WebFetch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "url parameter is required and must be a string",
		}, nil
	}

	fetchResult, err := t.fetchContent(ctx, url)
	success := err == nil

	result := &domain.ToolExecutionResult{
		ToolName:  "WebFetch",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
	}

	if err != nil {
		result.Error = err.Error()
	} else {
		result.Data = fetchResult
	}

	return result, nil
}

// Validate checks if the fetch tool arguments are valid
func (t *WebFetchTool) Validate(args map[string]interface{}) error {
	if !t.config.Tools.Enabled || !t.config.Tools.WebFetch.Enabled {
		return fmt.Errorf("fetch tool is not enabled")
	}

	url, ok := args["url"].(string)
	if !ok {
		return fmt.Errorf("url parameter is required and must be a string")
	}

	if err := t.validateURL(url); err != nil {
		return fmt.Errorf("URL validation failed: %w", err)
	}

	if format, ok := args["format"].(string); ok {
		if format != "text" && format != "json" {
			return fmt.Errorf("format must be 'text' or 'json'")
		}
	} else if args["format"] != nil {
		return fmt.Errorf("format parameter must be a string")
	}

	return nil
}

// IsEnabled returns whether the fetch tool is enabled
func (t *WebFetchTool) IsEnabled() bool {
	return t.enabled
}

// fetchContent fetches content from the given URL
func (t *WebFetchTool) fetchContent(ctx context.Context, url string) (*domain.FetchResult, error) {
	return t.fetchHTTPContent(ctx, url)
}

// fetchHTTPContent fetches content from a regular HTTP/HTTPS URL
func (t *WebFetchTool) fetchHTTPContent(ctx context.Context, url string) (*domain.FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch content: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var sizeWarning bool
	if resp.ContentLength > 0 && resp.ContentLength > t.config.Tools.WebFetch.Safety.MaxSize {
		sizeWarning = true
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, t.config.Tools.WebFetch.Safety.MaxSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var warning string
	originalSize := int64(len(body))
	if sizeWarning || int64(len(body)) >= t.config.Tools.WebFetch.Safety.MaxSize {
		warning = fmt.Sprintf("Content was truncated. Original size may exceed %d bytes, showing first %d bytes only.",
			t.config.Tools.WebFetch.Safety.MaxSize, len(body))
	}

	result := &domain.FetchResult{
		Content:     string(body),
		URL:         url,
		Status:      resp.StatusCode,
		Size:        originalSize,
		ContentType: resp.Header.Get("Content-Type"),
		Cached:      false,
		Warning:     warning,
		Metadata: map[string]string{
			"last_modified": resp.Header.Get("Last-Modified"),
			"etag":          resp.Header.Get("ETag"),
		},
	}

	return result, nil
}

// validateURL validates URL against security rules and whitelists
func (t *WebFetchTool) validateURL(url string) error {
	if url == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("protocol not allowed")
	}

	return t.validateURLDomain(url)
}

// validateURLDomain checks if URL domain is in whitelist
func (t *WebFetchTool) validateURLDomain(url string) error {
	for _, domain := range t.config.Tools.WebFetch.WhitelistedDomains {
		if strings.Contains(url, domain) {
			return nil
		}
	}

	return fmt.Errorf("domain not whitelisted")
}
