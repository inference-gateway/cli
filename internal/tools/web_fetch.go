package tools

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	download "github.com/inference-gateway/cli/internal/platform/download"
)

// WebFetchTool handles content fetching operations
type WebFetchTool struct {
	config    *config.Config
	enabled   bool
	client    *http.Client
	formatter agentinfra.BaseFormatter
}

// NewWebFetchTool creates a new fetch tool
func NewWebFetchTool(cfg *config.Config) *WebFetchTool {
	t := &WebFetchTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.WebFetch.Enabled,
		formatter: agentinfra.NewBaseFormatter(ToolWebFetch),
	}
	t.client = &http.Client{
		Timeout: time.Duration(cfg.Tools.WebFetch.Safety.Timeout) * time.Second,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return t.validateURL(req.URL.String())
		},
	}
	return t
}

// Manifest returns the tool's manifest with its configured require_approval.
func (t *WebFetchTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolWebFetch).WithRequireApproval(t.config.Tools.WebFetch.RequireApproval)
}

// Definition returns the tool definition for the LLM
func (t *WebFetchTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

// Execute runs the fetch tool with given arguments
func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled || !t.config.Tools.WebFetch.Enabled {
		return nil, fmt.Errorf("fetch tool is not enabled")
	}

	url, ok := args["url"].(string)
	if !ok {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolWebFetch,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "url parameter is required and must be a string",
		}, nil
	}

	if err := t.validateURL(url); err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolWebFetch,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("URL validation failed: %v", err),
		}, nil
	}

	wantsDownload, _ := args["download"].(bool)

	fetchResult, err := t.fetchContent(ctx, url)
	success := err == nil

	result := &agentdomain.ToolExecutionResult{
		ToolName:  ToolWebFetch,
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
	}

	if err != nil {
		result.Error = err.Error()
		return result, nil
	}

	isBinary := isBinaryContent(fetchResult.ContentType, fetchResult.Content)

	if wantsDownload || isBinary {
		filename := download.FilenameFromURL(url, fetchResult.ContentType)
		savedPath, saveErr := t.saveToFile(ctx, fetchResult, filename)
		if saveErr != nil {
			result.Error = fmt.Sprintf("failed to save file: %v", saveErr)
			result.Success = false
			return result, nil
		}
		fetchResult.SavedPath = savedPath
	}

	if isBinary {
		ct := cmp.Or(fetchResult.ContentType, "unknown type")
		_, _, inChannel := convdomain.ParseChannelSessionID(agentdomain.GetSessionID(ctx))
		if inChannel && fetchResult.SavedPath != "" && strings.HasPrefix(strings.ToLower(ct), "image/") {
			fetchResult.Content = fmt.Sprintf(
				"[image (%s, %s) saved to %s — the raw bytes are NOT shown to you. "+
					"To display it to the user, include exactly this on its own line "+
					"in your reply (no backticks, no code block): ![image](%s)]",
				ct, t.formatSize(fetchResult.Size), fetchResult.SavedPath, fetchResult.SavedPath)
		} else {
			fetchResult.Content = fmt.Sprintf(
				"[binary content (%s, %s) saved to disk — not inlined into context]",
				ct, t.formatSize(fetchResult.Size))
		}
	}
	result.Data = fetchResult

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
func (t *WebFetchTool) fetchContent(ctx context.Context, url string) (*agentdomain.FetchResult, error) {
	return t.fetchHTTPContent(ctx, url)
}

// fetchHTTPContent fetches content from a regular HTTP/HTTPS URL
func (t *WebFetchTool) fetchHTTPContent(ctx context.Context, url string) (*agentdomain.FetchResult, error) {
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

	result := &agentdomain.FetchResult{
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

// isBinaryContent reports whether a fetched body is non-text and therefore must
// not be inlined into the LLM context (raw bytes tokenize into garbage and can
// blow the context window). Content-Type is the primary signal; when it is
// missing we fall back to a UTF-8 validity check (a PNG is never valid UTF-8). A
// declared-textual type is trusted even if the bytes aren't valid UTF-8, so a
// mislabeled-charset page (e.g. latin-1 HTML) is still treated as text.
func isBinaryContent(contentType, body string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}

	switch {
	case ct == "":
		return !utf8.ValidString(body)
	case strings.HasPrefix(ct, "text/"),
		ct == "application/json",
		ct == "application/xml",
		ct == "application/javascript",
		ct == "application/ecmascript",
		strings.HasSuffix(ct, "+json"),
		strings.HasSuffix(ct, "+xml"):
		return false
	default:
		return true
	}
}

// validateURL validates URL against security rules and allowed lists
func (t *WebFetchTool) validateURL(url string) error {
	if url == "" {
		return fmt.Errorf("URL cannot be empty")
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("protocol not allowed")
	}

	return t.validateURLDomain(url)
}

// validateURLDomain checks if the URL's hostname is in the allowed list. The
// match is against the parsed hostname only — exact or as a subdomain suffix —
// never a substring of the whole URL, so `https://evil.com/?x=github.com` and
// `https://github.com.evil.com` are both rejected.
func (t *WebFetchTool) validateURLDomain(rawURL string) error {
	if t.config.IsA2AAgentHost(rawURL) {
		return nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("URL has no hostname")
	}

	for _, domain := range t.config.Tools.WebFetch.AllowedDomains {
		d := strings.ToLower(strings.TrimSpace(domain))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return nil
		}
	}

	return fmt.Errorf("domain not allowed (tools.web_fetch.allowed_domains: %s)",
		strings.Join(t.config.Tools.WebFetch.AllowedDomains, ", "))
}

// FormatResult formats tool execution results for different contexts
func (t *WebFetchTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterUI:
		return t.FormatForUI(result)
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *WebFetchTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	fetchResult, ok := result.Data.(*agentdomain.FetchResult)
	if !ok {
		if result.Success {
			return "Web fetch completed successfully"
		}
		return "Web fetch failed"
	}

	domain := t.extractDomain(fetchResult.URL)

	statusText := fmt.Sprintf("HTTP %d", fetchResult.Status)
	if fetchResult.Status >= 200 && fetchResult.Status < 300 {
		statusText = "OK"
	}

	sizeText := t.formatSize(fetchResult.Size)

	if fetchResult.SavedPath != "" {
		resultText := fmt.Sprintf("Fetched and saved to: %s (%s, %s)", fetchResult.SavedPath, statusText, sizeText)

		if fetchResult.Warning != "" {
			resultText += " [truncated]"
		}

		if fetchResult.Cached {
			resultText += " [cached]"
		}

		return resultText
	}

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
func (t *WebFetchTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", toolCall)
	fmt.Fprintf(&output, "└─ %s %s", statusIcon, preview)

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *WebFetchTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var dataContent string
	if result.Data != nil {
		dataContent = t.formatFetchData(result.Data)
	}
	return t.formatter.FormatExpanded(result, dataContent)
}

// formatFetchData formats web fetch-specific data
func (t *WebFetchTool) formatFetchData(data any) string {
	fetchResult, ok := data.(*agentdomain.FetchResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	fmt.Fprintf(&output, "URL: %s\n", fetchResult.URL)
	fmt.Fprintf(&output, "Status: %d\n", fetchResult.Status)
	fmt.Fprintf(&output, "Size: %s\n", t.formatSize(fetchResult.Size))
	fmt.Fprintf(&output, "Content Type: %s\n", fetchResult.ContentType)
	fmt.Fprintf(&output, "Cached: %t\n", fetchResult.Cached)

	if fetchResult.SavedPath != "" {
		fmt.Fprintf(&output, "Saved to: %s\n", fetchResult.SavedPath)
	}

	if fetchResult.Warning != "" {
		fmt.Fprintf(&output, "Warning: %s\n", fetchResult.Warning)
	}

	if len(fetchResult.Metadata) > 0 {
		output.WriteString("Metadata:\n")
		for key, value := range fetchResult.Metadata {
			if value != "" {
				fmt.Fprintf(&output, "  %s: %s\n", key, value)
			}
		}
	}

	if fetchResult.Content != "" {
		contentPreview := t.formatter.TruncateText(fetchResult.Content, 500)
		fmt.Fprintf(&output, "Content:\n%s\n", contentPreview)
	}

	return output.String()
}

// extractDomain extracts domain from URL for display
func (t *WebFetchTool) extractDomain(url string) string {
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

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *WebFetchTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *WebFetchTool) ShouldAlwaysExpand() bool {
	return false
}

// saveToFile saves the fetched content to disk in the session's artifacts
// directory
func (t *WebFetchTool) saveToFile(ctx context.Context, fetchResult *agentdomain.FetchResult, filename string) (string, error) {
	baseDir := t.config.SessionArtifactsDir(agentdomain.GetSessionID(ctx))

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", baseDir, err)
	}

	filename = filepath.Base(filename)

	fullPath := filepath.Join(baseDir, filename)

	if err := os.WriteFile(fullPath, []byte(fetchResult.Content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return fullPath, nil
	}

	return absPath, nil
}
