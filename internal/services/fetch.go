package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
)

// FetchService handles content fetching operations
type FetchService struct {
	config *config.Config
	client *http.Client
	cache  map[string]CacheEntry
}

// CacheEntry represents a cached fetch result
type CacheEntry struct {
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	URL       string    `json:"url"`
}

// GitHubReference represents a GitHub issue or PR reference
type GitHubReference struct {
	Owner  string
	Repo   string
	Number int
	Type   string // "issue" or "pull"
}

// NewFetchService creates a new FetchService
func NewFetchService(cfg *config.Config) *FetchService {
	return &FetchService{
		config: cfg,
		client: &http.Client{
			Timeout: time.Duration(cfg.Fetch.Safety.Timeout) * time.Second,
		},
		cache: make(map[string]CacheEntry),
	}
}

// ValidateURL checks if a URL is whitelisted for fetching
func (f *FetchService) ValidateURL(targetURL string) error {
	if !f.config.Fetch.Enabled {
		return fmt.Errorf("fetch tool is not enabled - use 'infer config fetch enable' to enable it")
	}

	if len(f.config.Fetch.WhitelistedURLs) == 0 && len(f.config.Fetch.URLPatterns) == 0 {
		return fmt.Errorf("no whitelisted sources configured - use 'infer config fetch add-source' to configure allowed URLs")
	}

	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTP and HTTPS URLs are allowed")
	}

	for _, whitelistedURL := range f.config.Fetch.WhitelistedURLs {
		if strings.HasPrefix(targetURL, whitelistedURL) {
			logger.Debug("URL matches whitelist", "url", targetURL, "pattern", whitelistedURL)
			return nil
		}
	}

	for _, pattern := range f.config.Fetch.URLPatterns {
		matched, err := regexp.MatchString(pattern, targetURL)
		if err != nil {
			logger.Error("Invalid URL pattern", "pattern", pattern, "error", err)
			continue
		}
		if matched {
			logger.Debug("URL matches pattern", "url", targetURL, "pattern", pattern)
			return nil
		}
	}

	return fmt.Errorf("URL not whitelisted: %s", targetURL)
}

// FetchContent fetches content from a URL or GitHub reference
func (f *FetchService) FetchContent(ctx context.Context, target string) (*domain.FetchResult, error) {
	if err := f.ValidateURL(target); err != nil {
		if githubRef, parseErr := f.parseGitHubReference(target); parseErr == nil {
			return f.fetchGitHubContent(ctx, githubRef)
		}
		return nil, err
	}

	if entry, found := f.getCachedContent(target); found {
		logger.Debug("Returning cached content", "url", target)
		return &domain.FetchResult{
			Content:     entry.Content,
			URL:         entry.URL,
			Cached:      true,
			ContentType: "text/plain",
		}, nil
	}

	return f.fetchURL(ctx, target)
}

// fetchURL performs the actual HTTP request
func (f *FetchService) fetchURL(ctx context.Context, targetURL string) (*domain.FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Inference-Gateway-CLI/1.0")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Error("Failed to close response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return &domain.FetchResult{
			URL:    targetURL,
			Status: resp.StatusCode,
		}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	if resp.ContentLength > f.config.Fetch.Safety.MaxSize {
		return nil, fmt.Errorf("content too large: %d bytes (max: %d bytes)", resp.ContentLength, f.config.Fetch.Safety.MaxSize)
	}

	limitedReader := io.LimitReader(resp.Body, f.config.Fetch.Safety.MaxSize)
	content, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	result := &domain.FetchResult{
		Content:     string(content),
		URL:         targetURL,
		Status:      resp.StatusCode,
		Size:        int64(len(content)),
		ContentType: resp.Header.Get("Content-Type"),
		Cached:      false,
	}

	f.cacheContent(targetURL, string(content))

	logger.Debug("Successfully fetched content", "url", targetURL, "size", len(content), "status", resp.StatusCode)
	return result, nil
}

// parseGitHubReference parses GitHub-style references like "github:owner/repo#123" or "source=github ticket_nr=123"
func (f *FetchService) parseGitHubReference(target string) (*GitHubReference, error) {
	if strings.HasPrefix(target, "github:") {
		return f.parseGitHubColonReference(target)
	}

	if strings.Contains(target, "source=github") {
		return f.parseGitHubParameterReference(target)
	}

	return nil, fmt.Errorf("not a GitHub reference")
}

// parseGitHubColonReference parses "github:owner/repo#123" format
func (f *FetchService) parseGitHubColonReference(target string) (*GitHubReference, error) {
	reference := strings.TrimPrefix(target, "github:")

	parts := strings.Split(reference, "#")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid GitHub reference format, expected: github:owner/repo#123")
	}

	repoPath := parts[0]
	numberStr := parts[1]

	repoParts := strings.Split(repoPath, "/")
	if len(repoParts) != 2 {
		return nil, fmt.Errorf("invalid repository format, expected: owner/repo")
	}

	number, err := strconv.Atoi(numberStr)
	if err != nil {
		return nil, fmt.Errorf("invalid issue/PR number: %w", err)
	}

	return &GitHubReference{
		Owner:  repoParts[0],
		Repo:   repoParts[1],
		Number: number,
		Type:   "issue", // Will be determined by GitHub API response
	}, nil
}

// parseGitHubParameterReference parses "source=github ticket_nr=123" format
func (f *FetchService) parseGitHubParameterReference(target string) (*GitHubReference, error) {
	var ticketNr int
	var owner, repo string

	parts := strings.Fields(target)
	for _, part := range parts {
		if strings.HasPrefix(part, "ticket_nr=") {
			numberStr := strings.TrimPrefix(part, "ticket_nr=")
			var err error
			ticketNr, err = strconv.Atoi(numberStr)
			if err != nil {
				return nil, fmt.Errorf("invalid ticket number: %w", err)
			}
		} else if strings.HasPrefix(part, "owner=") {
			owner = strings.TrimPrefix(part, "owner=")
		} else if strings.HasPrefix(part, "repo=") {
			repo = strings.TrimPrefix(part, "repo=")
		}
	}

	if ticketNr == 0 {
		return nil, fmt.Errorf("missing ticket_nr parameter")
	}

	if owner == "" {
		owner = "inference-gateway"
	}
	if repo == "" {
		repo = "cli"
	}

	return &GitHubReference{
		Owner:  owner,
		Repo:   repo,
		Number: ticketNr,
		Type:   "issue", // Will be determined by GitHub API response
	}, nil
}

// fetchGitHubContent fetches content from GitHub API
func (f *FetchService) fetchGitHubContent(ctx context.Context, ref *GitHubReference) (*domain.FetchResult, error) {
	if !f.config.Fetch.GitHub.Enabled {
		return nil, fmt.Errorf("GitHub integration is not enabled - use 'infer config fetch github enable' to enable it")
	}

	apiURL := fmt.Sprintf("%s/repos/%s/%s/issues/%d", f.config.Fetch.GitHub.BaseURL, ref.Owner, ref.Repo, ref.Number)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub API request: %w", err)
	}

	req.Header.Set("User-Agent", "Inference-Gateway-CLI/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	if f.config.Fetch.GitHub.Token != "" {
		req.Header.Set("Authorization", "token "+f.config.Fetch.GitHub.Token)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from GitHub API: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Error("Failed to close GitHub API response body", "error", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return &domain.FetchResult{
			URL:    apiURL,
			Status: resp.StatusCode,
		}, fmt.Errorf("GitHub API error: HTTP %d", resp.StatusCode)
	}

	var issue struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		PullRequest *struct{} `json:"pull_request,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub API response: %w", err)
	}

	ref.Type = "issue"
	if issue.PullRequest != nil {
		ref.Type = "pull"
	}

	var typeTitle string
	switch ref.Type {
	case "issue":
		typeTitle = "Issue"
	case "pull":
		typeTitle = "Pull Request"
	default:
		typeTitle = ref.Type
	}
	
	content := fmt.Sprintf("# %s #%d: %s\n\n**Author:** @%s\n**State:** %s\n**URL:** %s\n\n%s",
		typeTitle, issue.Number, issue.Title, issue.User.Login, issue.State, issue.HTMLURL, issue.Body)

	result := &domain.FetchResult{
		Content:     content,
		URL:         apiURL,
		Status:      resp.StatusCode,
		Size:        int64(len(content)),
		ContentType: "application/json",
		Cached:      false,
		Metadata: map[string]string{
			"github_type":   ref.Type,
			"github_owner":  ref.Owner,
			"github_repo":   ref.Repo,
			"github_number": strconv.Itoa(ref.Number),
			"github_title":  issue.Title,
			"github_state":  issue.State,
		},
	}

	cacheKey := fmt.Sprintf("github:%s/%s#%d", ref.Owner, ref.Repo, ref.Number)
	f.cacheContent(cacheKey, content)

	logger.Debug("Successfully fetched GitHub content", "type", ref.Type, "owner", ref.Owner, "repo", ref.Repo, "number", ref.Number)
	return result, nil
}

// getCachedContent retrieves content from cache if available and not expired
func (f *FetchService) getCachedContent(url string) (CacheEntry, bool) {
	if !f.config.Fetch.Cache.Enabled {
		return CacheEntry{}, false
	}

	entry, exists := f.cache[url]
	if !exists {
		return CacheEntry{}, false
	}

	if time.Since(entry.Timestamp) > time.Duration(f.config.Fetch.Cache.TTL)*time.Second {
		delete(f.cache, url)
		return CacheEntry{}, false
	}

	return entry, true
}

// cacheContent stores content in cache
func (f *FetchService) cacheContent(url, content string) {
	if !f.config.Fetch.Cache.Enabled {
		return
	}

	f.cache[url] = CacheEntry{
		Content:   content,
		Timestamp: time.Now(),
		URL:       url,
	}

	logger.Debug("Content cached", "url", url, "size", len(content))
}

// ClearCache clears all cached content
func (f *FetchService) ClearCache() {
	f.cache = make(map[string]CacheEntry)
	logger.Debug("Cache cleared")
}

// GetCacheStats returns cache statistics
func (f *FetchService) GetCacheStats() map[string]interface{} {
	totalSize := int64(0)
	for _, entry := range f.cache {
		totalSize += int64(len(entry.Content))
	}

	return map[string]interface{}{
		"entries":    len(f.cache),
		"total_size": totalSize,
		"enabled":    f.config.Fetch.Cache.Enabled,
		"max_size":   f.config.Fetch.Cache.MaxSize,
		"ttl":        f.config.Fetch.Cache.TTL,
	}
}
