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
			name: "no args opens A2A servers view",
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
			expectedSuccess: true,
			expectedContains: []string{
				"Opening A2A servers view",
			},
			expectedNotContain: []string{
				"A2A Agents",
				"Error:",
			},
		},
		{
			name: "explicit list argument opens A2A servers view",
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
			expectedSuccess: true,
			expectedContains: []string{
				"Opening A2A servers view",
			},
			expectedNotContain: []string{
				"A2A Agents",
				"Error:",
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
