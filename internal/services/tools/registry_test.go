package tools

import (
	"context"
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

type mockWebSearchService struct{}

func (m *mockWebSearchService) SearchGoogle(ctx context.Context, query string, maxResults int) (*domain.WebSearchResponse, error) {
	return &domain.WebSearchResponse{}, nil
}
func (m *mockWebSearchService) SearchDuckDuckGo(ctx context.Context, query string, maxResults int) (*domain.WebSearchResponse, error) {
	return &domain.WebSearchResponse{}, nil
}
func (m *mockWebSearchService) IsEnabled() bool     { return true }
func (m *mockWebSearchService) SetEnabled(bool)     {}

func createTestRegistry() *Registry {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
		Fetch: config.FetchConfig{
			Enabled: true,
		},
		WebSearch: config.WebSearchConfig{
			Enabled:       true,
			DefaultEngine: "duckduckgo",
			MaxResults:    10,
			Engines:       []string{"google", "duckduckgo"},
		},
	}

	return NewRegistry(cfg, &mockFileService{}, &mockFetchService{}, &mockWebSearchService{})
}

func TestRegistry_GetTool(t *testing.T) {
	registry := createTestRegistry()

	tests := []string{"Bash", "Read", "FileSearch", "Fetch", "WebSearch"}

	for _, toolName := range tests {
		t.Run(toolName, func(t *testing.T) {
			tool, err := registry.GetTool(toolName)
			if err != nil {
				t.Errorf("GetTool(%s) failed: %v", toolName, err)
			}

			if tool == nil {
				t.Errorf("GetTool(%s) returned nil tool", toolName)
			}

			if !tool.IsEnabled() {
				t.Errorf("Tool %s should be enabled", toolName)
			}
		})
	}
}

func TestRegistry_GetTool_Unknown(t *testing.T) {
	registry := createTestRegistry()

	_, err := registry.GetTool("UnknownTool")
	if err == nil {
		t.Error("Expected error for unknown tool")
	}
}

func TestRegistry_ListAvailableTools(t *testing.T) {
	registry := createTestRegistry()

	tools := registry.ListAvailableTools()
	if len(tools) == 0 {
		t.Error("Expected at least one tool to be available")
	}

	expectedTools := map[string]bool{
		"Bash":       true,
		"Read":       true,
		"FileSearch": true,
		"Fetch":      true,
		"WebSearch":  true,
	}

	for _, tool := range tools {
		if !expectedTools[tool] {
			t.Errorf("Unexpected tool in list: %s", tool)
		}
	}
}

func TestRegistry_GetToolDefinitions(t *testing.T) {
	registry := createTestRegistry()

	definitions := registry.GetToolDefinitions()
	if len(definitions) == 0 {
		t.Error("Expected at least one tool definition")
	}

	for _, def := range definitions {
		if def.Name == "" {
			t.Error("Tool definition should have a name")
		}
		if def.Description == "" {
			t.Error("Tool definition should have a description")
		}
		if def.Parameters == nil {
			t.Error("Tool definition should have parameters")
		}
	}
}

func TestRegistry_IsToolEnabled(t *testing.T) {
	registry := createTestRegistry()

	tests := []struct {
		toolName string
		expected bool
	}{
		{"Bash", true},
		{"Read", true},
		{"FileSearch", true},
		{"Fetch", true},
		{"WebSearch", true},
		{"UnknownTool", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			enabled := registry.IsToolEnabled(tt.toolName)
			if enabled != tt.expected {
				t.Errorf("IsToolEnabled(%s) = %v, expected %v", tt.toolName, enabled, tt.expected)
			}
		})
	}
}

func TestRegistry_DisabledTools(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
		Fetch: config.FetchConfig{
			Enabled: false, // Fetch disabled
		},
		WebSearch: config.WebSearchConfig{
			Enabled: false, // WebSearch disabled
		},
	}

	registry := NewRegistry(cfg, &mockFileService{}, &mockFetchService{}, &mockWebSearchService{})

	// Should not have Fetch and WebSearch tools
	tools := registry.ListAvailableTools()
	
	hasCore := false
	hasFetch := false
	hasWebSearch := false

	for _, tool := range tools {
		switch tool {
		case "Bash", "Read", "FileSearch":
			hasCore = true
		case "Fetch":
			hasFetch = true
		case "WebSearch":
			hasWebSearch = true
		}
	}

	if !hasCore {
		t.Error("Expected core tools to be available")
	}

	if hasFetch {
		t.Error("Fetch tool should not be available when disabled")
	}

	if hasWebSearch {
		t.Error("WebSearch tool should not be available when disabled")
	}
}