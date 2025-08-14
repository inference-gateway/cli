package services

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewWebSearchService(t *testing.T) {
	service := NewWebSearchService()

	if service == nil {
		t.Fatal("NewWebSearchService() returned nil")
	}

	if !service.IsEnabled() {
		t.Error("Expected web search service to be enabled by default")
	}

	if service.client == nil {
		t.Error("Expected HTTP client to be initialized")
	}

	if service.client.Timeout != 10*time.Second {
		t.Errorf("Expected timeout to be 10s, got %v", service.client.Timeout)
	}
}

func TestWebSearchService_IsEnabled(t *testing.T) {
	service := NewWebSearchService()

	// Test default enabled state
	if !service.IsEnabled() {
		t.Error("Expected service to be enabled by default")
	}

	// Test setting disabled
	service.SetEnabled(false)
	if service.IsEnabled() {
		t.Error("Expected service to be disabled after SetEnabled(false)")
	}

	// Test setting enabled
	service.SetEnabled(true)
	if !service.IsEnabled() {
		t.Error("Expected service to be enabled after SetEnabled(true)")
	}
}

func TestWebSearchService_SearchGoogle(t *testing.T) {
	service := NewWebSearchService()
	ctx := context.Background()

	tests := []struct {
		name       string
		query      string
		maxResults int
		wantError  bool
	}{
		{
			name:       "valid search",
			query:      "golang programming",
			maxResults: 5,
			wantError:  false,
		},
		{
			name:       "empty query",
			query:      "",
			maxResults: 10,
			wantError:  false, // Mock implementation should handle this
		},
		{
			name:       "large result limit",
			query:      "test query",
			maxResults: 50,
			wantError:  false,
		},
		{
			name:       "zero result limit",
			query:      "test query",
			maxResults: 0,
			wantError:  false, // Mock implementation should handle this
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.SearchGoogle(ctx, tt.query, tt.maxResults)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
				return
			}

			if !tt.wantError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			if result == nil {
				t.Error("Expected result to be non-nil")
				return
			}

			// Verify result structure
			if result.Query != tt.query {
				t.Errorf("Expected query %q, got %q", tt.query, result.Query)
			}

			if result.Engine != "google" {
				t.Errorf("Expected engine 'google', got %q", result.Engine)
			}

			if result.Time <= 0 {
				t.Error("Expected positive time duration")
			}

			// For mock implementation, we expect maxResults number of results
			if tt.maxResults > 0 && len(result.Results) != tt.maxResults {
				t.Errorf("Expected %d results, got %d", tt.maxResults, len(result.Results))
			}

			// Verify each result has required fields
			for i, res := range result.Results {
				if res.Title == "" {
					t.Errorf("Result %d has empty title", i)
				}
				if res.URL == "" {
					t.Errorf("Result %d has empty URL", i)
				}
				if res.Snippet == "" {
					t.Errorf("Result %d has empty snippet", i)
				}
			}
		})
	}
}

func TestWebSearchService_SearchDuckDuckGo(t *testing.T) {
	service := NewWebSearchService()
	ctx := context.Background()

	tests := []struct {
		name       string
		query      string
		maxResults int
		wantError  bool
	}{
		{
			name:       "valid search",
			query:      "golang programming",
			maxResults: 5,
			wantError:  false,
		},
		{
			name:       "programming query",
			query:      "web development",
			maxResults: 10,
			wantError:  false,
		},
		{
			name:       "single result",
			query:      "test query",
			maxResults: 1,
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.SearchDuckDuckGo(ctx, tt.query, tt.maxResults)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
				return
			}

			if !tt.wantError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			if result == nil {
				t.Error("Expected result to be non-nil")
				return
			}

			// Verify result structure
			if result.Query != tt.query {
				t.Errorf("Expected query %q, got %q", tt.query, result.Query)
			}

			if result.Engine != "duckduckgo" {
				t.Errorf("Expected engine 'duckduckgo', got %q", result.Engine)
			}

			if result.Time <= 0 {
				t.Error("Expected positive time duration")
			}

			// Verify results exist
			if len(result.Results) == 0 {
				t.Error("Expected at least one search result")
			}

			// Verify each result has required fields
			for i, res := range result.Results {
				if res.Title == "" {
					t.Errorf("Result %d has empty title", i)
				}
				if res.URL == "" {
					t.Errorf("Result %d has empty URL", i)
				}
				if res.Snippet == "" {
					t.Errorf("Result %d has empty snippet", i)
				}
			}
		})
	}
}

func TestWebSearchService_generateMockResults(t *testing.T) {
	service := NewWebSearchService()

	tests := []struct {
		name       string
		query      string
		maxResults int
		engine     string
	}{
		{
			name:       "google mock results",
			query:      "test query",
			maxResults: 5,
			engine:     "google",
		},
		{
			name:       "duckduckgo mock results",
			query:      "programming",
			maxResults: 10,
			engine:     "duckduckgo",
		},
		{
			name:       "single result",
			query:      "single",
			maxResults: 1,
			engine:     "google",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := service.generateMockResults(tt.query, tt.maxResults, tt.engine)

			if len(results) != tt.maxResults {
				t.Errorf("Expected %d results, got %d", tt.maxResults, len(results))
			}

			for i, result := range results {
				if result.Title == "" {
					t.Errorf("Result %d has empty title", i)
				}
				if result.URL == "" {
					t.Errorf("Result %d has empty URL", i)
				}
				if result.Snippet == "" {
					t.Errorf("Result %d has empty snippet", i)
				}

				// Verify content includes query and engine
				expectedTitle := "Search Result " + fmt.Sprintf("%d", i+1) + " for '" + tt.query + "'"
				if result.Title != expectedTitle {
					t.Errorf("Expected title %q, got %q", expectedTitle, result.Title)
				}

				expectedURL := "https://example.com/" + tt.engine + "-result-" + fmt.Sprintf("%d", i+1)
				if result.URL != expectedURL {
					t.Errorf("Expected URL %q, got %q", expectedURL, result.URL)
				}
			}
		})
	}
}

func TestWebSearchService_contextTimeout(t *testing.T) {
	service := NewWebSearchService()

	// Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Wait for context to timeout
	time.Sleep(2 * time.Millisecond)

	// This should still work with mock implementation
	result, err := service.SearchGoogle(ctx, "test query", 5)

	// Mock implementation should work even with cancelled context
	// In a real implementation, this would likely return an error
	if err != nil {
		t.Logf("Context timeout handled appropriately: %v", err)
	}

	if result != nil && result.Engine != "google" {
		t.Errorf("Expected engine 'google', got %q", result.Engine)
	}
}
