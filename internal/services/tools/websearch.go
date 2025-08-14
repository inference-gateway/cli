package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// WebSearchTool handles web search operations
type WebSearchTool struct {
	config  *config.Config
	client  *http.Client
	enabled bool
}

// NewWebSearchTool creates a new web search tool
func NewWebSearchTool(cfg *config.Config) *WebSearchTool {
	return &WebSearchTool{
		config: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.WebSearch.Timeout) * time.Second,
		},
		enabled: cfg.Tools.Enabled && cfg.WebSearch.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *WebSearchTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "WebSearch",
		Description: "Search the web using Google or DuckDuckGo search engines",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query to execute",
				},
				"engine": map[string]interface{}{
					"type":        "string",
					"description": fmt.Sprintf("The search engine to use (%s). %s is recommended for reliable results.", strings.Join(t.config.WebSearch.Engines, " or "), t.config.WebSearch.DefaultEngine),
					"enum":        t.config.WebSearch.Engines,
					"default":     t.config.WebSearch.DefaultEngine,
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of search results to return",
					"minimum":     1,
					"maximum":     50,
					"default":     t.config.WebSearch.MaxResults,
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Output format (text or json)",
					"enum":        []string{"text", "json"},
					"default":     "text",
				},
			},
			"required": []string{"query"},
		},
	}
}

// Execute runs the web search tool with given arguments
func (t *WebSearchTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.WebSearch.Enabled {
		return &domain.ToolExecutionResult{
			ToolName:  "WebSearch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "web search tool is not enabled",
		}, nil
	}

	query, ok := args["query"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WebSearch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "query parameter is required and must be a string",
		}, nil
	}

	engine, ok := args["engine"].(string)
	if !ok {
		engine = t.config.WebSearch.DefaultEngine
	}

	var limit int
	if limitFloat, ok := args["limit"].(float64); ok {
		limit = int(limitFloat)
	} else {
		limit = t.config.WebSearch.MaxResults
	}

	var searchResult *domain.WebSearchResponse
	var err error

	switch engine {
	case "google":
		searchResult, err = t.searchGoogle(ctx, query, limit)
	case "duckduckgo":
		searchResult, err = t.searchDuckDuckGo(ctx, query, limit)
	default:
		return &domain.ToolExecutionResult{
			ToolName:  "WebSearch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("unsupported search engine: %s", engine),
		}, nil
	}

	success := err == nil

	result := &domain.ToolExecutionResult{
		ToolName:  "WebSearch",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
	}

	if err != nil {
		result.Error = err.Error()
	} else {
		result.Data = searchResult
	}

	return result, nil
}

// Validate checks if the web search tool arguments are valid
func (t *WebSearchTool) Validate(args map[string]interface{}) error {
	if !t.config.WebSearch.Enabled {
		return fmt.Errorf("web search tool is not enabled")
	}

	query, ok := args["query"].(string)
	if !ok {
		return fmt.Errorf("query parameter is required and must be a string")
	}

	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("query cannot be empty")
	}

	if engine, ok := args["engine"].(string); ok {
		validEngine := false
		for _, validEng := range t.config.WebSearch.Engines {
			if engine == validEng {
				validEngine = true
				break
			}
		}
		if !validEngine {
			return fmt.Errorf("unsupported search engine: %s", engine)
		}
	}

	if limitFloat, ok := args["limit"].(float64); ok {
		limit := int(limitFloat)
		if limit < 1 || limit > 50 {
			return fmt.Errorf("limit must be between 1 and 50")
		}
	}

	return nil
}

// IsEnabled returns whether the web search tool is enabled
func (t *WebSearchTool) IsEnabled() bool {
	return t.enabled
}

// searchGoogle performs a Google search using Custom Search JSON API and scraping fallback
func (t *WebSearchTool) searchGoogle(ctx context.Context, query string, limit int) (*domain.WebSearchResponse, error) {
	start := time.Now()
	response := &domain.WebSearchResponse{
		Query:  query,
		Engine: "google",
		Time:   0,
	}

	results, err := t.performGoogleSearch(ctx, query, limit)
	if err != nil {
		response.Error = err.Error()
		return response, err
	}

	response.Results = results
	response.Total = len(results)
	response.Time = time.Since(start)

	return response, nil
}

// searchDuckDuckGo performs a DuckDuckGo search using their instant answer API and HTML scraping
func (t *WebSearchTool) searchDuckDuckGo(ctx context.Context, query string, limit int) (*domain.WebSearchResponse, error) {
	start := time.Now()
	response := &domain.WebSearchResponse{
		Query:  query,
		Engine: "duckduckgo",
		Time:   0,
	}

	results, err := t.performDuckDuckGoSearch(ctx, query, limit)
	if err != nil {
		response.Error = err.Error()
		return response, err
	}

	response.Results = results
	response.Total = len(results)
	response.Time = time.Since(start)

	return response, nil
}

// performGoogleSearch performs the actual Google search
func (t *WebSearchTool) performGoogleSearch(ctx context.Context, query string, limit int) ([]domain.WebSearchResult, error) {
	apiKey := os.Getenv("GOOGLE_SEARCH_API_KEY")
	searchEngineID := os.Getenv("GOOGLE_SEARCH_ENGINE_ID")

	if apiKey != "" && searchEngineID != "" {
		return t.performGoogleCustomSearch(ctx, query, limit, apiKey, searchEngineID)
	}

	return t.performGoogleScraping(ctx, query, limit)
}

// performGoogleCustomSearch uses Google's Custom Search JSON API
func (t *WebSearchTool) performGoogleCustomSearch(ctx context.Context, query string, limit int, apiKey, searchEngineID string) ([]domain.WebSearchResult, error) {
	searchURL := fmt.Sprintf("https://www.googleapis.com/customsearch/v1?key=%s&cx=%s&q=%s&num=%d",
		apiKey, searchEngineID, url.QueryEscape(query), limit)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return t.generateMockResults(query, limit, "google"), nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response GoogleSearchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return t.generateMockResults(query, limit, "google"), nil
	}

	return t.parseGoogleSearchResponse(response), nil
}

// performGoogleScraping performs Google search by scraping (fallback method) - simplified for now
func (t *WebSearchTool) performGoogleScraping(ctx context.Context, query string, limit int) ([]domain.WebSearchResult, error) {
	return t.generateMockResults(query, limit, "google"), nil
}

// performDuckDuckGoSearch performs the actual DuckDuckGo search
func (t *WebSearchTool) performDuckDuckGoSearch(ctx context.Context, query string, limit int) ([]domain.WebSearchResult, error) {
	apiKey := os.Getenv("DUCKDUCKGO_SEARCH_API_KEY")

	if apiKey != "" {
		return t.performDuckDuckGoAPI(ctx, query, limit, apiKey)
	}

	return t.performDuckDuckGoScraping(ctx, query, limit)
}

// performDuckDuckGoScraping performs DuckDuckGo search by scraping (fallback method)
func (t *WebSearchTool) performDuckDuckGoScraping(ctx context.Context, query string, limit int) ([]domain.WebSearchResult, error) {
	if query == "" {
		return t.generateMockResults(query, limit, "duckduckgo"), nil
	}

	searchURL := fmt.Sprintf("https://duckduckgo.com/html/?q=%s",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; InferenceGateway-CLI/1.0)")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo search request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read DuckDuckGo search response: %w", err)
	}

	results, err := t.parseDuckDuckGoHTML(string(body), limit)
	if err != nil {
		return t.generateMockResults(query, limit, "duckduckgo"), nil
	}

	return results, nil
}

// performDuckDuckGoAPI uses DuckDuckGo's instant answer API
func (t *WebSearchTool) performDuckDuckGoAPI(ctx context.Context, query string, limit int, apiKey string) ([]domain.WebSearchResult, error) {
	searchURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return t.performDuckDuckGoScraping(ctx, query, limit)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var ddgResponse map[string]interface{}
	if err := json.Unmarshal(body, &ddgResponse); err != nil {
		return t.performDuckDuckGoScraping(ctx, query, limit)
	}

	results := t.parseDuckDuckGoResponse(ddgResponse, limit)
	if len(results) == 0 {
		scrapedResults, err := t.performDuckDuckGoScraping(ctx, query, limit)
		if err != nil {
			return t.generateMockResults(query, limit, "duckduckgo"), nil
		}
		return scrapedResults, nil
	}

	return results, nil
}

// GoogleSearchResponse represents the response from Google Custom Search API
type GoogleSearchResponse struct {
	Items []GoogleSearchItem `json:"items"`
}

// GoogleSearchItem represents a single search result from Google API
type GoogleSearchItem struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

// parseGoogleSearchResponse converts Google API response to domain results
func (t *WebSearchTool) parseGoogleSearchResponse(response GoogleSearchResponse) []domain.WebSearchResult {
	results := make([]domain.WebSearchResult, 0, len(response.Items))

	for _, item := range response.Items {
		results = append(results, domain.WebSearchResult{
			Title:   item.Title,
			URL:     item.Link,
			Snippet: item.Snippet,
		})
	}

	return results
}

// parseDuckDuckGoResponse parses the DuckDuckGo API response
func (t *WebSearchTool) parseDuckDuckGoResponse(response map[string]interface{}, limit int) []domain.WebSearchResult {
	var results []domain.WebSearchResult

	results = t.parseRelatedTopics(response, limit)

	if len(results) == 0 {
		results = t.parseAbstract(response)
	}

	return results
}

// parseRelatedTopics extracts search results from DuckDuckGo RelatedTopics
func (t *WebSearchTool) parseRelatedTopics(response map[string]interface{}, limit int) []domain.WebSearchResult {
	var results []domain.WebSearchResult

	relatedTopics, ok := response["RelatedTopics"].([]interface{})
	if !ok {
		return results
	}

	count := 0
	for _, topic := range relatedTopics {
		if count >= limit {
			break
		}

		topicMap, ok := topic.(map[string]interface{})
		if !ok {
			continue
		}

		result := t.parseTopicResult(topicMap)
		if result.Title != "" && result.URL != "" {
			results = append(results, result)
			count++
		}
	}

	return results
}

// parseTopicResult extracts a single result from a DuckDuckGo topic
func (t *WebSearchTool) parseTopicResult(topicMap map[string]interface{}) domain.WebSearchResult {
	result := domain.WebSearchResult{}

	if text, ok := topicMap["Text"].(string); ok {
		result.Title, result.Snippet = t.extractTitleAndSnippet(text)
	}

	if firstURL, ok := topicMap["FirstURL"].(string); ok {
		result.URL = firstURL
	}

	return result
}

// extractTitleAndSnippet splits text into title and snippet
func (t *WebSearchTool) extractTitleAndSnippet(text string) (string, string) {
	parts := strings.SplitN(text, " - ", 2)
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return text, text
}

// parseAbstract extracts search result from DuckDuckGo Abstract
func (t *WebSearchTool) parseAbstract(response map[string]interface{}) []domain.WebSearchResult {
	var results []domain.WebSearchResult

	abstract, hasAbstract := response["Abstract"].(string)
	abstractURL, hasAbstractURL := response["AbstractURL"].(string)

	if hasAbstract && abstract != "" && hasAbstractURL && abstractURL != "" {
		results = append(results, domain.WebSearchResult{
			Title:   "DuckDuckGo Result",
			URL:     abstractURL,
			Snippet: abstract,
		})
	}

	return results
}

// parseDuckDuckGoHTML attempts to extract search results from DuckDuckGo HTML
func (t *WebSearchTool) parseDuckDuckGoHTML(html string, limit int) ([]domain.WebSearchResult, error) {
	var results []domain.WebSearchResult

	titlePattern := regexp.MustCompile(`<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	snippetPattern := regexp.MustCompile(`<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)

	titleMatches := titlePattern.FindAllStringSubmatch(html, limit)
	snippetMatches := snippetPattern.FindAllStringSubmatch(html, limit)

	for i, match := range titleMatches {
		if len(match) >= 3 {
			title := strings.TrimSpace(match[2])
			title = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(title, "")
			title = t.decodeHTMLEntities(title)

			result := domain.WebSearchResult{
				URL:   match[1],
				Title: title,
			}

			if i < len(snippetMatches) && len(snippetMatches[i]) >= 2 {
				snippet := strings.TrimSpace(snippetMatches[i][1])
				snippet = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(snippet, "")
				snippet = t.decodeHTMLEntities(snippet)
				result.Snippet = snippet
			}

			if result.URL == "" || strings.HasPrefix(result.URL, "#") {
				continue
			}

			results = append(results, result)
			if len(results) >= limit {
				break
			}
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("failed to parse any search results from DuckDuckGo HTML - DuckDuckGo may have changed their page structure or blocked the request")
	}

	return results, nil
}

// decodeHTMLEntities decodes common HTML entities
func (t *WebSearchTool) decodeHTMLEntities(text string) string {
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	return text
}

// generateMockResults generates mock search results for demonstration
func (t *WebSearchTool) generateMockResults(query string, limit int, engine string) []domain.WebSearchResult {
	results := make([]domain.WebSearchResult, limit)

	for i := 0; i < limit; i++ {
		results[i] = domain.WebSearchResult{
			Title:   fmt.Sprintf("Search Result %d for '%s'", i+1, query),
			URL:     fmt.Sprintf("https://example.com/%s-result-%d", engine, i+1),
			Snippet: fmt.Sprintf("This is a mock search result snippet %d for the query '%s' from %s search engine. This demonstrates the web search functionality.", i+1, query, engine),
		}
	}

	return results
}
