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
	config    *config.Config
	enabled   bool
	client    *http.Client
	formatter domain.BaseFormatter
}

// NewWebFetchTool creates a new fetch tool
func NewWebFetchTool(cfg *config.Config) *WebFetchTool {
	return &WebFetchTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.WebFetch.Enabled,
		client: &http.Client{
			Timeout: time.Duration(cfg.Tools.WebFetch.Safety.Timeout) * time.Second,
		},
		formatter: domain.NewBaseFormatter("WebFetch"),
	}
}

// Definition returns the tool definition for the LLM
func (t *WebFetchTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "WebFetch",
		Description: "Fetch content from whitelisted URLs references.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch content from",
				},
				"format": map[string]any{
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
func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
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
func (t *WebFetchTool) Validate(args map[string]any) error {
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

// FormatResult formats tool execution results for different contexts
func (t *WebFetchTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *WebFetchTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	fetchResult, ok := result.Data.(*domain.FetchResult)
	if !ok {
		if result.Success {
			return "Web fetch completed successfully"
		}
		return "Web fetch failed"
	}

	// Extract domain from URL for display
	domain := t.extractDomain(fetchResult.URL)

	statusText := fmt.Sprintf("HTTP %d", fetchResult.Status)
	if fetchResult.Status >= 200 && fetchResult.Status < 300 {
		statusText = "OK"
	}

	sizeText := t.formatSize(fetchResult.Size)
	resultText := fmt.Sprintf("Fetched from %s (%s, %s)", domain, statusText, sizeText)

	if fetchResult.Warning != "" {
		resultText += " [truncated]"
	}

	if fetchResult.Cached {
		resultText += " [cached]"
	}

	return resultText
}

// FormatForUI formats the result for UI display
func (t *WebFetchTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, preview))

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *WebFetchTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatFetchData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatFetchData formats web fetch-specific data
func (t *WebFetchTool) formatFetchData(data any) string {
	fetchResult, ok := data.(*domain.FetchResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("URL: %s\n", fetchResult.URL))
	output.WriteString(fmt.Sprintf("Status: %d\n", fetchResult.Status))
	output.WriteString(fmt.Sprintf("Size: %s\n", t.formatSize(fetchResult.Size)))
	output.WriteString(fmt.Sprintf("Content Type: %s\n", fetchResult.ContentType))
	output.WriteString(fmt.Sprintf("Cached: %t\n", fetchResult.Cached))

	if fetchResult.Warning != "" {
		output.WriteString(fmt.Sprintf("Warning: %s\n", fetchResult.Warning))
	}

	// Metadata
	if len(fetchResult.Metadata) > 0 {
		output.WriteString("Metadata:\n")
		for key, value := range fetchResult.Metadata {
			if value != "" {
				output.WriteString(fmt.Sprintf("  %s: %s\n", key, value))
			}
		}
	}

	// Show content preview
	if fetchResult.Content != "" {
		contentPreview := t.formatter.TruncateText(fetchResult.Content, 500)
		output.WriteString(fmt.Sprintf("Content:\n%s\n", contentPreview))
	}

	return output.String()
}

// extractDomain extracts domain from URL for display
func (t *WebFetchTool) extractDomain(url string) string {
	// Simple domain extraction
	if strings.HasPrefix(url, "http://") {
		url = url[7:]
	} else if strings.HasPrefix(url, "https://") {
		url = url[8:]
	}

	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}

	return url
}

// formatSize formats byte size in human-readable format
func (t *WebFetchTool) formatSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d bytes", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	} else if size < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(size)/(1024*1024*1024))
}
