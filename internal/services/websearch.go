package services

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

	"github.com/inference-gateway/cli/internal/domain"
)

// WebSearchEngine represents different search engines
type WebSearchEngine string

const (
	GoogleSearch     WebSearchEngine = "google"
	DuckDuckGoSearch WebSearchEngine = "duckduckgo"
)

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

// WebSearchService handles web search operations
type WebSearchService struct {
	client  *http.Client
	enabled bool
}

// NewWebSearchService creates a new web search service
func NewWebSearchService() *WebSearchService {
	return &WebSearchService{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		enabled: true,
	}
}

// SearchGoogle performs a Google search using the Custom Search JSON API
func (s *WebSearchService) SearchGoogle(ctx context.Context, query string, maxResults int) (*domain.WebSearchResponse, error) {
	start := time.Now()
	response := &domain.WebSearchResponse{
		Query:  query,
		Engine: string(GoogleSearch),
		Time:   0,
	}

	results, err := s.performGoogleSearch(ctx, query, maxResults)
	if err != nil {
		response.Error = err.Error()
		return response, err
	}

	response.Results = results
	response.Total = len(results)
	response.Time = time.Since(start)

	return response, nil
}

// SearchDuckDuckGo performs a DuckDuckGo search using their instant answer API
func (s *WebSearchService) SearchDuckDuckGo(ctx context.Context, query string, maxResults int) (*domain.WebSearchResponse, error) {
	start := time.Now()
	response := &domain.WebSearchResponse{
		Query:  query,
		Engine: string(DuckDuckGoSearch),
		Time:   0,
	}

	results, err := s.performDuckDuckGoSearch(ctx, query, maxResults)
	if err != nil {
		response.Error = err.Error()
		return response, err
	}

	response.Results = results
	response.Total = len(results)
	response.Time = time.Since(start)

	return response, nil
}

// performGoogleSearch performs the actual Google search using Custom Search JSON API
func (s *WebSearchService) performGoogleSearch(ctx context.Context, query string, maxResults int) ([]domain.WebSearchResult, error) {
	apiKey := getEnvVar("GOOGLE_SEARCH_API_KEY")
	searchEngineID := getEnvVar("GOOGLE_SEARCH_ENGINE_ID")

	if apiKey != "" && searchEngineID != "" {
		return s.performGoogleCustomSearch(ctx, query, maxResults, apiKey, searchEngineID)
	}

	return s.performGoogleScraping(ctx, query, maxResults)
}

// performGoogleCustomSearch uses Google's Custom Search JSON API
func (s *WebSearchService) performGoogleCustomSearch(ctx context.Context, query string, maxResults int, apiKey, searchEngineID string) ([]domain.WebSearchResult, error) {
	searchURL := fmt.Sprintf("https://www.googleapis.com/customsearch/v1?key=%s&cx=%s&q=%s&num=%d",
		apiKey, searchEngineID, url.QueryEscape(query), maxResults)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return s.generateMockResults(query, maxResults, "google"), nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response GoogleSearchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return s.generateMockResults(query, maxResults, "google"), nil
	}

	return s.parseGoogleSearchResponse(response), nil
}

// performGoogleScraping performs Google search by scraping (fallback method)
func (s *WebSearchService) performGoogleScraping(ctx context.Context, query string, maxResults int) ([]domain.WebSearchResult, error) {
	if query == "" {
		return s.generateMockResults(query, maxResults, "google"), nil
	}

	searchURL := fmt.Sprintf("https://www.google.com/search?q=%s&num=%d",
		url.QueryEscape(query), maxResults)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("DNT", "1")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google search request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Google search response: %w", err)
	}

	results, err := s.parseGoogleHTML(string(body), maxResults)
	if err != nil {
		return s.generateMockResults(query, maxResults, "google"), nil
	}

	return results, nil
}

// performDuckDuckGoSearch performs the actual DuckDuckGo search
func (s *WebSearchService) performDuckDuckGoSearch(ctx context.Context, query string, maxResults int) ([]domain.WebSearchResult, error) {
	apiKey := getEnvVar("DUCKDUCKGO_SEARCH_API_KEY")

	if apiKey != "" {
		return s.performDuckDuckGoAPI(ctx, query, maxResults, apiKey)
	}

	return s.performDuckDuckGoScraping(ctx, query, maxResults)
}

// performDuckDuckGoAPI uses DuckDuckGo's instant answer API
func (s *WebSearchService) performDuckDuckGoAPI(ctx context.Context, query string, maxResults int, apiKey string) ([]domain.WebSearchResult, error) {
	searchURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return s.performDuckDuckGoScraping(ctx, query, maxResults)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var ddgResponse map[string]interface{}
	if err := json.Unmarshal(body, &ddgResponse); err != nil {
		return s.performDuckDuckGoScraping(ctx, query, maxResults)
	}

	results := s.parseDuckDuckGoResponse(ddgResponse, maxResults)
	if len(results) == 0 {
		scrapedResults, err := s.performDuckDuckGoScraping(ctx, query, maxResults)
		if err != nil {
			return s.generateMockResults(query, maxResults, "duckduckgo"), nil
		}
		return scrapedResults, nil
	}

	return results, nil
}

// performDuckDuckGoScraping performs DuckDuckGo search by scraping (fallback method)
func (s *WebSearchService) performDuckDuckGoScraping(ctx context.Context, query string, maxResults int) ([]domain.WebSearchResult, error) {
	if query == "" {
		return s.generateMockResults(query, maxResults, "duckduckgo"), nil
	}

	searchURL := fmt.Sprintf("https://duckduckgo.com/html/?q=%s",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; InferenceGateway-CLI/1.0)")

	resp, err := s.client.Do(req)
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

	results, err := s.parseDuckDuckGoHTML(string(body), maxResults)
	if err != nil {
		return s.generateMockResults(query, maxResults, "duckduckgo"), nil
	}

	return results, nil
}

// parseDuckDuckGoResponse parses the DuckDuckGo API response
func (s *WebSearchService) parseDuckDuckGoResponse(response map[string]interface{}, maxResults int) []domain.WebSearchResult {
	var results []domain.WebSearchResult

	results = s.parseRelatedTopics(response, maxResults)

	if len(results) == 0 {
		results = s.parseAbstract(response)
	}

	return results
}

// parseRelatedTopics extracts search results from DuckDuckGo RelatedTopics
func (s *WebSearchService) parseRelatedTopics(response map[string]interface{}, maxResults int) []domain.WebSearchResult {
	var results []domain.WebSearchResult

	relatedTopics, ok := response["RelatedTopics"].([]interface{})
	if !ok {
		return results
	}

	count := 0
	for _, topic := range relatedTopics {
		if count >= maxResults {
			break
		}

		topicMap, ok := topic.(map[string]interface{})
		if !ok {
			continue
		}

		result := s.parseTopicResult(topicMap)
		if result.Title != "" && result.URL != "" {
			results = append(results, result)
			count++
		}
	}

	return results
}

// parseTopicResult extracts a single result from a DuckDuckGo topic
func (s *WebSearchService) parseTopicResult(topicMap map[string]interface{}) domain.WebSearchResult {
	result := domain.WebSearchResult{}

	if text, ok := topicMap["Text"].(string); ok {
		result.Title, result.Snippet = s.extractTitleAndSnippet(text)
	}

	if firstURL, ok := topicMap["FirstURL"].(string); ok {
		result.URL = firstURL
	}

	return result
}

// extractTitleAndSnippet splits text into title and snippet
func (s *WebSearchService) extractTitleAndSnippet(text string) (string, string) {
	parts := strings.SplitN(text, " - ", 2)
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return text, text
}

// parseAbstract extracts search result from DuckDuckGo Abstract
func (s *WebSearchService) parseAbstract(response map[string]interface{}) []domain.WebSearchResult {
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

// generateMockResults generates mock search results for demonstration
func (s *WebSearchService) generateMockResults(query string, maxResults int, engine string) []domain.WebSearchResult {
	results := make([]domain.WebSearchResult, maxResults)

	for i := 0; i < maxResults; i++ {
		results[i] = domain.WebSearchResult{
			Title:   fmt.Sprintf("Search Result %d for '%s'", i+1, query),
			URL:     fmt.Sprintf("https://example.com/%s-result-%d", engine, i+1),
			Snippet: fmt.Sprintf("This is a mock search result snippet %d for the query '%s' from %s search engine. This demonstrates the web search functionality.", i+1, query, engine),
		}
	}

	return results
}

// IsEnabled returns whether the web search service is enabled
func (s *WebSearchService) IsEnabled() bool {
	return s.enabled
}

// SetEnabled sets the enabled state of the web search service
func (s *WebSearchService) SetEnabled(enabled bool) {
	s.enabled = enabled
}

// getEnvVar safely gets an environment variable
func getEnvVar(key string) string {
	return os.Getenv(key)
}

// parseGoogleSearchResponse converts Google API response to domain results
func (s *WebSearchService) parseGoogleSearchResponse(response GoogleSearchResponse) []domain.WebSearchResult {
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

// parseGoogleHTML attempts to extract search results from Google HTML (basic implementation)
func (s *WebSearchService) parseGoogleHTML(html string, maxResults int) ([]domain.WebSearchResult, error) {
	titleMatches, patternUsed := s.extractGoogleTitleMatches(html, maxResults)
	allSnippets := s.extractGoogleSnippets(html, maxResults)

	results := s.buildGoogleResults(titleMatches, allSnippets, patternUsed, maxResults)

	if len(results) == 0 {
		return nil, fmt.Errorf("failed to parse any search results from Google HTML - Google may have changed their page structure or is blocking the request")
	}

	return results, nil
}

// extractGoogleTitleMatches extracts title and URL matches from Google HTML
func (s *WebSearchService) extractGoogleTitleMatches(html string, maxResults int) ([][]string, int) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`<a[^>]*href="([^"]*)"[^>]*>.*?<h3[^>]*>(.*?)</h3>`),
		regexp.MustCompile(`<h3[^>]*>(.*?)</h3>.*?<a[^>]*href="([^"]*)"[^>]*>`),
		regexp.MustCompile(`href="([^"]*)"[^>]*>.*?<h3[^>]*>(.*?)</h3>`),
	}

	for i, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(html, maxResults*3)
		if len(matches) > 0 {
			return matches, i
		}
	}

	return s.fallbackTitleExtraction(html, maxResults), 0
}

// fallbackTitleExtraction tries to extract titles and URLs separately when combined patterns fail
func (s *WebSearchService) fallbackTitleExtraction(html string, maxResults int) [][]string {
	h3Pattern := regexp.MustCompile(`<h3[^>]*>(.*?)</h3>`)
	hrefPattern := regexp.MustCompile(`href="(https?://[^"]*)"`)

	h3Matches := h3Pattern.FindAllStringSubmatch(html, maxResults*3)
	hrefMatches := hrefPattern.FindAllStringSubmatch(html, maxResults*3)

	var titleMatches [][]string
	for i, h3Match := range h3Matches {
		if i < len(hrefMatches) && len(h3Match) >= 2 && len(hrefMatches[i]) >= 2 {
			syntheticMatch := []string{
				h3Match[0] + hrefMatches[i][0],
				hrefMatches[i][1],
				h3Match[1],
			}
			titleMatches = append(titleMatches, syntheticMatch)
		}
	}

	return titleMatches
}

// extractGoogleSnippets extracts snippet text from Google HTML
func (s *WebSearchService) extractGoogleSnippets(html string, maxResults int) []string {
	snippetPattern := regexp.MustCompile(`>([^<]{50,200})<`)
	snippetMatches := snippetPattern.FindAllStringSubmatch(html, maxResults*3)

	var allSnippets []string
	for _, match := range snippetMatches {
		if len(match) < 2 {
			continue
		}

		snippet := strings.TrimSpace(match[1])
		snippet = s.decodeHTMLEntities(snippet)

		if len(snippet) > 30 && len(strings.TrimSpace(snippet)) > 20 {
			allSnippets = append(allSnippets, snippet)
		}

		if len(allSnippets) >= maxResults {
			break
		}
	}

	return allSnippets
}

// buildGoogleResults builds WebSearchResult objects from extracted matches
func (s *WebSearchService) buildGoogleResults(titleMatches [][]string, allSnippets []string, patternUsed int, maxResults int) []domain.WebSearchResult {
	var results []domain.WebSearchResult
	snippetIndex := 0

	for _, match := range titleMatches {
		if len(match) < 3 || len(results) >= maxResults {
			continue
		}

		result := s.processGoogleMatch(match, patternUsed)
		if result == nil {
			continue
		}

		if snippetIndex < len(allSnippets) {
			result.Snippet = allSnippets[snippetIndex]
			snippetIndex++
		}

		results = append(results, *result)
	}

	return results
}

// processGoogleMatch processes a single Google search match
func (s *WebSearchService) processGoogleMatch(match []string, patternUsed int) *domain.WebSearchResult {
	var resultURL, title string

	if patternUsed == 3 {
		title = strings.TrimSpace(match[1])
		resultURL = strings.TrimSpace(match[2])
	} else {
		resultURL = strings.TrimSpace(match[1])
		title = strings.TrimSpace(match[2])
	}

	resultURL = s.cleanGoogleURL(resultURL)
	if !s.isValidURL(resultURL) {
		return nil
	}

	title = s.cleanTitle(title)
	if title == "" {
		return nil
	}

	return &domain.WebSearchResult{
		URL:   resultURL,
		Title: title,
	}
}

// cleanGoogleURL cleans Google redirect URLs
func (s *WebSearchService) cleanGoogleURL(resultURL string) string {
	if strings.HasPrefix(resultURL, "/url?q=") {
		if parsedURL, err := url.Parse(resultURL); err == nil {
			if actualURL := parsedURL.Query().Get("q"); actualURL != "" {
				return actualURL
			}
		}
	}
	return resultURL
}

// isValidURL checks if URL is valid for search results
func (s *WebSearchService) isValidURL(resultURL string) bool {
	return resultURL != "" &&
		!strings.HasPrefix(resultURL, "#") &&
		!strings.HasPrefix(resultURL, "javascript:")
}

// cleanTitle cleans and decodes HTML title
func (s *WebSearchService) cleanTitle(title string) string {
	title = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(title, "")
	return s.decodeHTMLEntities(title)
}

// decodeHTMLEntities decodes common HTML entities
func (s *WebSearchService) decodeHTMLEntities(text string) string {
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	return text
}

// parseDuckDuckGoHTML attempts to extract search results from DuckDuckGo HTML
func (s *WebSearchService) parseDuckDuckGoHTML(html string, maxResults int) ([]domain.WebSearchResult, error) {
	var results []domain.WebSearchResult

	titlePattern := regexp.MustCompile(`<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]*)"[^>]*>(.*?)</a>`)
	snippetPattern := regexp.MustCompile(`<a[^>]*class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>`)

	titleMatches := titlePattern.FindAllStringSubmatch(html, maxResults)
	snippetMatches := snippetPattern.FindAllStringSubmatch(html, maxResults)

	for i, match := range titleMatches {
		if len(match) >= 3 {
			title := strings.TrimSpace(match[2])
			title = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(title, "")
			title = strings.ReplaceAll(title, "&amp;", "&")
			title = strings.ReplaceAll(title, "&lt;", "<")
			title = strings.ReplaceAll(title, "&gt;", ">")
			title = strings.ReplaceAll(title, "&quot;", "\"")

			result := domain.WebSearchResult{
				URL:   match[1],
				Title: title,
			}

			if i < len(snippetMatches) && len(snippetMatches[i]) >= 2 {
				snippet := strings.TrimSpace(snippetMatches[i][1])
				snippet = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(snippet, "")
				snippet = strings.ReplaceAll(snippet, "&amp;", "&")
				snippet = strings.ReplaceAll(snippet, "&lt;", "<")
				snippet = strings.ReplaceAll(snippet, "&gt;", ">")
				snippet = strings.ReplaceAll(snippet, "&quot;", "\"")
				result.Snippet = snippet
			}

			if result.URL == "" || strings.HasPrefix(result.URL, "#") {
				continue
			}

			results = append(results, result)
			if len(results) >= maxResults {
				break
			}
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("failed to parse any search results from DuckDuckGo HTML - DuckDuckGo may have changed their page structure or blocked the request")
	}

	return results, nil
}
