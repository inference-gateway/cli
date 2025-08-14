package tools

import (
	"context"
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestWebSearchTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WebSearch: config.WebSearchToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWebSearchTool(cfg)
	def := tool.Definition()

	if def.Name != "WebSearch" {
		t.Errorf("Expected tool name 'WebSearch', got %s", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestWebSearchTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name              string
		toolsEnabled      bool
		webSearchEnabled  bool
		expectedState     bool
	}{
		{
			name:              "enabled when both tools and websearch enabled",
			toolsEnabled:      true,
			webSearchEnabled:  true,
			expectedState:     true,
		},
		{
			name:              "disabled when tools disabled",
			toolsEnabled:      false,
			webSearchEnabled:  true,
			expectedState:     false,
		},
		{
			name:              "disabled when websearch disabled",
			toolsEnabled:      true,
			webSearchEnabled:  false,
			expectedState:     false,
		},
		{
			name:              "disabled when both disabled",
			toolsEnabled:      false,
			webSearchEnabled:  false,
			expectedState:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
					WebSearch: config.WebSearchToolConfig{
						Enabled: tt.webSearchEnabled,
					},
				},
			}

			tool := NewWebSearchTool(cfg)
			if tool.IsEnabled() != tt.expectedState {
				t.Errorf("Expected IsEnabled() = %v, got %v", tt.expectedState, tool.IsEnabled())
			}
		})
	}
}

func TestWebSearchTool_Validate(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WebSearch: config.WebSearchToolConfig{
				Enabled:       true,
				DefaultEngine: "duckduckgo",
				MaxResults:    10,
				Engines:       []string{"duckduckgo", "google"},
			},
		},
	}

	tool := NewWebSearchTool(cfg)

	tests := []struct {
		name      string
		args      map[string]interface{}
		wantError bool
	}{
		{
			name: "valid query with default engine",
			args: map[string]interface{}{
				"query": "golang testing",
			},
			wantError: false,
		},
		{
			name: "valid query with specified engine",
			args: map[string]interface{}{
				"query":  "golang testing",
				"engine": "google",
			},
			wantError: false,
		},
		{
			name: "valid query with limit",
			args: map[string]interface{}{
				"query": "golang testing",
				"limit": 5,
			},
			wantError: false,
		},
		{
			name: "invalid engine",
			args: map[string]interface{}{
				"query":  "golang testing",
				"engine": "bing",
			},
			wantError: true,
		},
		{
			name: "missing query",
			args: map[string]interface{}{
				"engine": "google",
			},
			wantError: true,
		},
		{
			name: "empty query",
			args: map[string]interface{}{
				"query": "",
			},
			wantError: true,
		},
		{
			name: "query wrong type",
			args: map[string]interface{}{
				"query": 123,
			},
			wantError: true,
		},
		{
			name: "limit too high",
			args: map[string]interface{}{
				"query": "golang testing",
				"limit": 100,
			},
			wantError: true,
		},
		{
			name: "limit wrong type",
			args: map[string]interface{}{
				"query": "golang testing",
				"limit": "5",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestWebSearchTool_Execute_Disabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
			WebSearch: config.WebSearchToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWebSearchTool(cfg)
	ctx := context.Background()

	args := map[string]interface{}{
		"query": "golang testing",
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when tool is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when tool is disabled")
	}
}
