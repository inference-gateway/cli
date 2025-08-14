package tools

import (
	"context"
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestFetchTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
		Fetch: config.FetchConfig{
			Enabled: true,
		},
	}

	tool := NewFetchTool(cfg)
	def := tool.Definition()

	if def.Name != "Fetch" {
		t.Errorf("Expected tool name 'Fetch', got %s", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestFetchTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name          string
		toolsEnabled  bool
		fetchEnabled  bool
		expectedState bool
	}{
		{
			name:          "enabled when both tools and fetch enabled",
			toolsEnabled:  true,
			fetchEnabled:  true,
			expectedState: true,
		},
		{
			name:          "disabled when tools disabled",
			toolsEnabled:  false,
			fetchEnabled:  true,
			expectedState: false,
		},
		{
			name:          "disabled when fetch disabled",
			toolsEnabled:  true,
			fetchEnabled:  false,
			expectedState: false,
		},
		{
			name:          "disabled when both disabled",
			toolsEnabled:  false,
			fetchEnabled:  false,
			expectedState: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
				},
				Fetch: config.FetchConfig{
					Enabled: tt.fetchEnabled,
				},
			}

			tool := NewFetchTool(cfg)
			if tool.IsEnabled() != tt.expectedState {
				t.Errorf("Expected IsEnabled() = %v, got %v", tt.expectedState, tool.IsEnabled())
			}
		})
	}
}

func TestFetchTool_Validate(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
		Fetch: config.FetchConfig{
			Enabled: true,
			WhitelistedDomains: []string{
				"api.github.com",
				"httpbin.org",
				"github.com",
			},
		},
	}

	tool := NewFetchTool(cfg)

	tests := []struct {
		name      string
		args      map[string]interface{}
		wantError bool
	}{
		{
			name: "valid whitelisted URL",
			args: map[string]interface{}{
				"url": "https://httpbin.org/json",
			},
			wantError: false,
		},
		{
			name: "valid URL with format",
			args: map[string]interface{}{
				"url":    "https://httpbin.org/json",
				"format": "json",
			},
			wantError: false,
		},
		{
			name: "valid pattern URL",
			args: map[string]interface{}{
				"url": "https://github.com/owner/repo/issues/123",
			},
			wantError: false,
		},
		{
			name: "valid GitHub reference",
			args: map[string]interface{}{
				"url": "github:owner/repo#123",
			},
			wantError: false,
		},
		{
			name:      "missing URL",
			args:      map[string]interface{}{},
			wantError: true,
		},
		{
			name: "empty URL",
			args: map[string]interface{}{
				"url": "",
			},
			wantError: true,
		},
		{
			name: "URL wrong type",
			args: map[string]interface{}{
				"url": 123,
			},
			wantError: true,
		},
		{
			name: "non-whitelisted URL",
			args: map[string]interface{}{
				"url": "https://example.com/test",
			},
			wantError: true,
		},
		{
			name: "invalid format",
			args: map[string]interface{}{
				"url":    "https://httpbin.org/json",
				"format": "xml",
			},
			wantError: true,
		},
		{
			name: "format wrong type",
			args: map[string]interface{}{
				"url":    "https://httpbin.org/json",
				"format": 123,
			},
			wantError: true,
		},
		{
			name: "malformed GitHub reference",
			args: map[string]interface{}{
				"url": "github:invalid",
			},
			wantError: true,
		},
		{
			name: "file:// protocol not allowed",
			args: map[string]interface{}{
				"url": "file:///etc/passwd",
			},
			wantError: true,
		},
		{
			name: "ftp:// protocol not allowed",
			args: map[string]interface{}{
				"url": "ftp://example.com/file.txt",
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

func TestFetchTool_Execute_Disabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
		},
		Fetch: config.FetchConfig{
			Enabled: true,
		},
	}

	tool := NewFetchTool(cfg)
	ctx := context.Background()

	args := map[string]interface{}{
		"url": "https://httpbin.org/json",
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when tool is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when tool is disabled")
	}
}

func TestFetchTool_Execute_FetchDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
		Fetch: config.FetchConfig{
			Enabled: false,
		},
	}

	tool := NewFetchTool(cfg)
	ctx := context.Background()

	args := map[string]interface{}{
		"url": "https://httpbin.org/json",
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when fetch is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when fetch is disabled")
	}
}
