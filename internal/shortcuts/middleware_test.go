package shortcuts

import (
	"context"
	"strings"
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestA2AShortcut_GetName(t *testing.T) {
	mockConfig := &config.Config{}
	shortcut := NewA2AShortcut(mockConfig, nil)

	expected := "a2a"
	actual := shortcut.GetName()

	if actual != expected {
		t.Errorf("Expected name %s, got %s", expected, actual)
	}
}

func TestA2AShortcut_GetDescription(t *testing.T) {
	mockConfig := &config.Config{}
	shortcut := NewA2AShortcut(mockConfig, nil)

	expected := "List connected A2A servers"
	actual := shortcut.GetDescription()

	if actual != expected {
		t.Errorf("Expected description %s, got %s", expected, actual)
	}
}

func TestA2AShortcut_GetUsage(t *testing.T) {
	mockConfig := &config.Config{}
	shortcut := NewA2AShortcut(mockConfig, nil)

	expected := "/a2a [list]"
	actual := shortcut.GetUsage()

	if actual != expected {
		t.Errorf("Expected usage %s, got %s", expected, actual)
	}
}

func TestA2AShortcut_CanExecute(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "no arguments allowed",
			args:     []string{},
			expected: true,
		},
		{
			name:     "list argument allowed",
			args:     []string{"list"},
			expected: true,
		},
		{
			name:     "other argument not allowed",
			args:     []string{"other"},
			expected: false,
		},
		{
			name:     "multiple arguments not allowed",
			args:     []string{"arg1", "arg2"},
			expected: false,
		},
	}

	mockConfig := &config.Config{}
	shortcut := NewA2AShortcut(mockConfig, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortcut.CanExecute(tt.args)
			if result != tt.expected {
				t.Errorf("Expected CanExecute(%v) = %v, got %v", tt.args, tt.expected, result)
			}
		})
	}
}

func TestA2AShortcut_Execute(t *testing.T) {
	tests := []struct {
		name               string
		config             *config.Config
		args               []string
		expectedSuccess    bool
		expectedContains   []string
		expectedNotContain []string
	}{
		{
			name: "no args defaults to list - no client configured",
			config: &config.Config{
				Gateway: config.GatewayConfig{
					URL:     "http://localhost:8080",
					APIKey:  "test-api-key",
					Timeout: 30,
					Middlewares: config.MiddlewaresConfig{
						A2A: true,
					},
				},
			},
			args:            []string{},
			expectedSuccess: false,
			expectedContains: []string{
				"A2A Agents",
				"Error:** SDK client not configured",
			},
			expectedNotContain: []string{
				"A2A Server Status",
			},
		},
		{
			name: "explicit list argument - no client configured",
			config: &config.Config{
				Gateway: config.GatewayConfig{
					URL:     "http://localhost:8080",
					APIKey:  "test-api-key",
					Timeout: 30,
					Middlewares: config.MiddlewaresConfig{
						A2A: true,
					},
				},
			},
			args:            []string{"list"},
			expectedSuccess: false,
			expectedContains: []string{
				"A2A Agents",
				"Error:** SDK client not configured",
			},
			expectedNotContain: []string{
				"A2A Server Status",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shortcut := NewA2AShortcut(tt.config, nil)

			result, err := shortcut.Execute(context.Background(), tt.args)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if result.Success != tt.expectedSuccess {
				t.Errorf("Expected success %v, got %v", tt.expectedSuccess, result.Success)
			}

			output := result.Output
			for _, expected := range tt.expectedContains {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain '%s', but it didn't. Output: %s", expected, output)
				}
			}

			for _, notExpected := range tt.expectedNotContain {
				if strings.Contains(output, notExpected) {
					t.Errorf("Expected output to not contain '%s', but it did. Output: %s", notExpected, output)
				}
			}
		})
	}
}

func TestMCPShortcut_GetName(t *testing.T) {
	mockConfig := &config.Config{}
	shortcut := NewMCPShortcut(mockConfig, nil)

	expected := "mcp"
	actual := shortcut.GetName()

	if actual != expected {
		t.Errorf("Expected name %s, got %s", expected, actual)
	}
}

func TestMCPShortcut_GetDescription(t *testing.T) {
	mockConfig := &config.Config{}
	shortcut := NewMCPShortcut(mockConfig, nil)

	expected := "List connected MCP servers"
	actual := shortcut.GetDescription()

	if actual != expected {
		t.Errorf("Expected description %s, got %s", expected, actual)
	}
}

func TestMCPShortcut_GetUsage(t *testing.T) {
	mockConfig := &config.Config{}
	shortcut := NewMCPShortcut(mockConfig, nil)

	expected := "/mcp"
	actual := shortcut.GetUsage()

	if actual != expected {
		t.Errorf("Expected usage %s, got %s", expected, actual)
	}
}

func TestMCPShortcut_CanExecute(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "no arguments allowed",
			args:     []string{},
			expected: true,
		},
		{
			name:     "one argument not allowed",
			args:     []string{"arg1"},
			expected: false,
		},
		{
			name:     "multiple arguments not allowed",
			args:     []string{"arg1", "arg2"},
			expected: false,
		},
	}

	mockConfig := &config.Config{}
	shortcut := NewMCPShortcut(mockConfig, nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortcut.CanExecute(tt.args)
			if result != tt.expected {
				t.Errorf("Expected CanExecute(%v) = %v, got %v", tt.args, tt.expected, result)
			}
		})
	}
}

func TestMCPShortcut_Execute(t *testing.T) {
	tests := []struct {
		name               string
		config             *config.Config
		expectedSuccess    bool
		expectedContains   []string
		expectedNotContain []string
	}{
		{
			name: "gateway configured with MCP enabled",
			config: &config.Config{
				Gateway: config.GatewayConfig{
					URL:     "http://localhost:8080",
					APIKey:  "test-api-key",
					Timeout: 30,
					Middlewares: config.MiddlewaresConfig{
						MCP: true,
					},
				},
			},
			expectedSuccess: true,
			expectedContains: []string{
				"MCP Server Status",
				"Gateway URL:** http://localhost:8080",
				"MCP Middleware:** Enabled",
				"API Key:** Configured",
				"Connection Timeout:** 30 seconds",
				"Model Context Protocol",
			},
			expectedNotContain: []string{
				"Gateway URL not configured",
				"MCP Middleware:** Disabled",
				"API Key:** Not configured",
			},
		},
		{
			name: "gateway configured with MCP disabled",
			config: &config.Config{
				Gateway: config.GatewayConfig{
					URL:     "http://localhost:8080",
					APIKey:  "test-api-key",
					Timeout: 30,
					Middlewares: config.MiddlewaresConfig{
						MCP: false,
					},
				},
			},
			expectedSuccess: true,
			expectedContains: []string{
				"MCP Server Status",
				"Gateway URL:** http://localhost:8080",
				"MCP Middleware:** Disabled",
				"API Key:** Configured",
				"Connection Timeout:** 30 seconds",
			},
			expectedNotContain: []string{
				"Gateway URL not configured",
				"MCP Middleware:** Enabled",
				"API Key:** Not configured",
			},
		},
		{
			name: "gateway not configured",
			config: &config.Config{
				Gateway: config.GatewayConfig{
					URL:     "",
					APIKey:  "",
					Timeout: 30,
					Middlewares: config.MiddlewaresConfig{
						MCP: true,
					},
				},
			},
			expectedSuccess: false,
			expectedContains: []string{
				"Gateway URL not configured",
				"Configure the gateway URL in your config",
				"gateway:",
				"url: http://your-gateway-url:8080",
			},
			expectedNotContain: []string{
				"Gateway URL:**",
				"MCP Middleware:**",
			},
		},
		{
			name: "gateway configured without API key",
			config: &config.Config{
				Gateway: config.GatewayConfig{
					URL:     "http://localhost:8080",
					APIKey:  "",
					Timeout: 120,
					Middlewares: config.MiddlewaresConfig{
						MCP: true,
					},
				},
			},
			expectedSuccess: true,
			expectedContains: []string{
				"MCP Server Status",
				"Gateway URL:** http://localhost:8080",
				"MCP Middleware:** Enabled",
				"API Key:** Not configured - connection may fail",
				"Connection Timeout:** 120 seconds",
			},
			expectedNotContain: []string{
				"Gateway URL not configured",
				"API Key:** Configured",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shortcut := NewMCPShortcut(tt.config, nil)

			result, err := shortcut.Execute(context.Background(), []string{})

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if result.Success != tt.expectedSuccess {
				t.Errorf("Expected success %v, got %v", tt.expectedSuccess, result.Success)
			}

			output := result.Output
			for _, expected := range tt.expectedContains {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain '%s', but it didn't. Output: %s", expected, output)
				}
			}

			for _, notExpected := range tt.expectedNotContain {
				if strings.Contains(output, notExpected) {
					t.Errorf("Expected output to not contain '%s', but it did. Output: %s", notExpected, output)
				}
			}
		})
	}
}
