package services

import (
	"context"
	"strings"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// Mock implementations for testing
type mockFileService struct{}

func (m *mockFileService) ListProjectFiles() ([]string, error)  { return []string{}, nil }
func (m *mockFileService) ReadFile(path string) (string, error) { return "", nil }
func (m *mockFileService) ReadFileLines(path string, startLine, endLine int) (string, error) {
	return "", nil
}
func (m *mockFileService) ValidateFile(path string) error { return nil }
func (m *mockFileService) GetFileInfo(path string) (domain.FileInfo, error) {
	return domain.FileInfo{}, nil
}

type mockFetchService struct{}

func (m *mockFetchService) ValidateURL(url string) error { return nil }
func (m *mockFetchService) FetchContent(ctx context.Context, target string) (*domain.FetchResult, error) {
	return &domain.FetchResult{}, nil
}
func (m *mockFetchService) ClearCache()                           {}
func (m *mockFetchService) GetCacheStats() map[string]interface{} { return nil }

func createTestToolService() *LLMToolService {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
		WebSearch: config.WebSearchConfig{
			Enabled:       true,
			DefaultEngine: "google",
			MaxResults:    10,
			Engines:       []string{"google", "duckduckgo"},
			Timeout:       10,
		},
	}

	fileService := &mockFileService{}
	fetchService := &mockFetchService{}
	webSearchService := NewWebSearchService()

	return NewLLMToolService(cfg, fileService, fetchService, webSearchService)
}

func TestLLMToolService_ListTools_IncludesWebSearch(t *testing.T) {
	toolService := createTestToolService()
	tools := toolService.ListTools()

	var webSearchTool *domain.ToolDefinition
	for _, tool := range tools {
		if tool.Name == "WebSearch" {
			webSearchTool = &tool
			break
		}
	}

	if webSearchTool == nil {
		t.Fatal("WebSearch tool not found in ListTools() result")
	}

	if webSearchTool.Description == "" {
		t.Error("WebSearch tool has empty description")
	}

	if webSearchTool.Parameters == nil {
		t.Fatal("WebSearch tool has nil parameters")
	}

	params, ok := webSearchTool.Parameters.(map[string]interface{})
	if !ok {
		t.Fatal("WebSearch tool parameters is not a map")
	}

	properties, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("WebSearch tool properties is not a map")
	}

	requiredParams := []string{"query"}
	if required, ok := params["required"].([]string); ok {
		for _, param := range requiredParams {
			found := false
			for _, req := range required {
				if req == param {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Required parameter %q not found in required list", param)
			}
		}
	} else {
		t.Error("WebSearch tool required parameters is not a string slice")
	}

	if _, exists := properties["query"]; !exists {
		t.Error("Query parameter not found in properties")
	}

	engine, hasEngine := properties["engine"]
	if !hasEngine {
		return
	}

	engineMap, ok := engine.(map[string]interface{})
	if !ok {
		return
	}

	enum, hasEnum := engineMap["enum"]
	if !hasEnum {
		return
	}

	enumSlice, ok := enum.([]string)
	if !ok {
		t.Error("Engine enum is not a string slice")
		return
	}

	expectedEngines := []string{"google", "duckduckgo"}
	for _, expected := range expectedEngines {
		found := false
		for _, actual := range enumSlice {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected engine %q not found in enum", expected)
		}
	}
}

func TestLLMToolService_IsToolEnabled_WebSearch(t *testing.T) {
	toolService := createTestToolService()

	if !toolService.IsToolEnabled("WebSearch") {
		t.Error("WebSearch tool should be enabled")
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
		WebSearch: config.WebSearchConfig{
			Enabled: false,
		},
	}

	disabledService := NewLLMToolService(cfg, &mockFileService{}, &mockFetchService{}, NewWebSearchService())

	if disabledService.IsToolEnabled("WebSearch") {
		t.Error("WebSearch tool should be disabled when WebSearch.Enabled is false")
	}
}

func TestLLMToolService_ValidateTool_WebSearch(t *testing.T) {
	toolService := createTestToolService()

	tests := []struct {
		name      string
		args      map[string]interface{}
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid args with query only",
			args: map[string]interface{}{
				"query": "golang programming",
			},
			wantError: false,
		},
		{
			name: "valid args with all parameters",
			args: map[string]interface{}{
				"query":  "web development",
				"engine": "google",
				"limit":  float64(5),
				"format": "json",
			},
			wantError: false,
		},
		{
			name: "missing query",
			args: map[string]interface{}{
				"engine": "google",
			},
			wantError: true,
			errorMsg:  "query parameter is required",
		},
		{
			name: "empty query",
			args: map[string]interface{}{
				"query": "",
			},
			wantError: true,
			errorMsg:  "query cannot be empty",
		},
		{
			name: "whitespace only query",
			args: map[string]interface{}{
				"query": "   ",
			},
			wantError: true,
			errorMsg:  "query cannot be empty",
		},
		{
			name: "invalid engine",
			args: map[string]interface{}{
				"query":  "test",
				"engine": "bing",
			},
			wantError: true,
			errorMsg:  "unsupported search engine",
		},
		{
			name: "limit too low",
			args: map[string]interface{}{
				"query": "test",
				"limit": float64(0),
			},
			wantError: true,
			errorMsg:  "limit must be between 1 and 50",
		},
		{
			name: "limit too high",
			args: map[string]interface{}{
				"query": "test",
				"limit": float64(100),
			},
			wantError: true,
			errorMsg:  "limit must be between 1 and 50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := toolService.ValidateTool("WebSearch", tt.args)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
				return
			}

			if !tt.wantError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			if tt.wantError && err != nil && !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("Expected error message to contain %q, got: %v", tt.errorMsg, err)
			}
		})
	}
}

func TestLLMToolService_ExecuteTool_WebSearch(t *testing.T) {
	toolService := createTestToolService()
	ctx := context.Background()

	tests := []struct {
		name      string
		args      map[string]interface{}
		wantError bool
	}{
		{
			name: "google search",
			args: map[string]interface{}{
				"query":  "golang programming",
				"engine": "google",
				"limit":  float64(5),
			},
			wantError: false,
		},
		{
			name: "duckduckgo search",
			args: map[string]interface{}{
				"query":  "web development",
				"engine": "duckduckgo",
				"limit":  float64(3),
			},
			wantError: false,
		},
		{
			name: "default engine and limit",
			args: map[string]interface{}{
				"query": "programming tutorial",
			},
			wantError: false,
		},
		{
			name: "json format",
			args: map[string]interface{}{
				"query":  "test query",
				"format": "json",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := toolService.ExecuteTool(ctx, "WebSearch", tt.args)

			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
				return
			}

			if !tt.wantError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
				return
			}

			if result == nil {
				t.Error("Expected non-nil result")
				return
			}

			if !result.Success {
				t.Errorf("Expected successful execution but got error: %s", result.Error)
				return
			}

			if result.ToolName != "WebSearch" {
				t.Errorf("Expected tool name 'WebSearch' but got: %s", result.ToolName)
			}

			if result.Data == nil {
				t.Error("Expected result data but got nil")
				return
			}

			// Verify that the web search response is present
			if _, ok := result.Data.(*domain.WebSearchResponse); !ok {
				t.Error("Expected WebSearchResponse data type")
			}
		})
	}
}

func TestLLMToolService_WebSearch_DisabledService(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
		WebSearch: config.WebSearchConfig{
			Enabled: false,
		},
	}

	toolService := NewLLMToolService(cfg, &mockFileService{}, &mockFetchService{}, NewWebSearchService())

	tools := toolService.ListTools()
	for _, tool := range tools {
		if tool.Name == "WebSearch" {
			t.Error("WebSearch tool should not appear when disabled")
		}
	}

	if toolService.IsToolEnabled("WebSearch") {
		t.Error("WebSearch tool should not be enabled when disabled")
	}

	args := map[string]interface{}{
		"query": "test",
	}

	err := toolService.ValidateTool("WebSearch", args)
	if err == nil {
		t.Error("Expected validation to fail when web search is disabled")
	}

	ctx := context.Background()
	result, err := toolService.ExecuteTool(ctx, "WebSearch", args)
	if err == nil {
		t.Error("Expected execution to fail when web search is disabled")
	}
	if result != nil {
		t.Error("Expected nil result when execution fails")
	}
}
