package domain

import "time"

// FetchResult represents the result of a fetch operation
type FetchResult struct {
	Content     string            `json:"content"`
	URL         string            `json:"url"`
	Status      int               `json:"status"`
	Size        int64             `json:"size"`
	ContentType string            `json:"content_type"`
	Cached      bool              `json:"cached"`
	SavedPath   string            `json:"saved_path,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Warning     string            `json:"warning,omitempty"`
}

// WebSearchResult represents a single search result
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebSearchResponse represents the complete search response
type WebSearchResponse struct {
	Query   string            `json:"query"`
	Engine  string            `json:"engine"`
	Results []WebSearchResult `json:"results"`
	Total   int               `json:"total"`
	Time    time.Duration     `json:"time"`
	Error   string            `json:"error,omitempty"`
}
