package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestFileSearchTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}

	tool := NewFileSearchTool(cfg)
	def := tool.Definition()

	if def.Name != "FileSearch" {
		t.Errorf("Expected tool name 'FileSearch', got %s", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestFileSearchTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name          string
		toolsEnabled  bool
		expectedState bool
	}{
		{
			name:          "enabled when tools enabled",
			toolsEnabled:  true,
			expectedState: true,
		},
		{
			name:          "disabled when tools disabled",
			toolsEnabled:  false,
			expectedState: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
				},
			}

			tool := NewFileSearchTool(cfg)
			if tool.IsEnabled() != tt.expectedState {
				t.Errorf("Expected IsEnabled() = %v, got %v", tt.expectedState, tool.IsEnabled())
			}
		})
	}
}

func TestFileSearchTool_Validate(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}

	tool := NewFileSearchTool(cfg)

	tests := []struct {
		name      string
		args      map[string]interface{}
		wantError bool
	}{
		{
			name: "valid pattern",
			args: map[string]interface{}{
				"pattern": "\\.go$",
			},
			wantError: false,
		},
		{
			name: "valid pattern with options",
			args: map[string]interface{}{
				"pattern":        "\\.go$",
				"include_dirs":   true,
				"case_sensitive": false,
			},
			wantError: false,
		},
		{
			name:      "missing pattern",
			args:      map[string]interface{}{},
			wantError: true,
		},
		{
			name: "empty pattern",
			args: map[string]interface{}{
				"pattern": "",
			},
			wantError: true,
		},
		{
			name: "pattern wrong type",
			args: map[string]interface{}{
				"pattern": 123,
			},
			wantError: true,
		},
		{
			name: "invalid regex pattern",
			args: map[string]interface{}{
				"pattern": "[invalid",
			},
			wantError: true,
		},
		{
			name: "include_dirs wrong type",
			args: map[string]interface{}{
				"pattern":      "\\.go$",
				"include_dirs": "true",
			},
			wantError: true,
		},
		{
			name: "case_sensitive wrong type",
			args: map[string]interface{}{
				"pattern":        "\\.go$",
				"case_sensitive": "false",
			},
			wantError: true,
		},
		{
			name: "max_depth wrong type",
			args: map[string]interface{}{
				"pattern":   "\\.go$",
				"max_depth": "5",
			},
			wantError: true,
		},
		{
			name: "negative max_depth",
			args: map[string]interface{}{
				"pattern":   "\\.go$",
				"max_depth": -1,
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

func TestFileSearchTool_Execute(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}

	tool := NewFileSearchTool(cfg)
	ctx := context.Background()

	tmpDir := t.TempDir()

	testFiles := []string{
		"main.go",
		"utils.go",
		"config.yaml",
		"test_file.txt",
		"README.md",
		"subdir/another.go",
		"subdir/config.json",
	}

	for _, file := range testFiles {
		fullPath := filepath.Join(tmpDir, file)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", fullPath, err)
		}
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("Failed to restore working directory: %v", err)
		}
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	t.Run("find go files", func(t *testing.T) {
		args := map[string]interface{}{
			"pattern": "\\.go$",
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if result.ToolName != "FileSearch" {
			t.Errorf("Expected tool name 'FileSearch', got %s", result.ToolName)
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}
	})

	t.Run("find config files", func(t *testing.T) {
		args := map[string]interface{}{
			"pattern": "config\\.(yaml|json)$",
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}
	})

	t.Run("case insensitive search", func(t *testing.T) {
		args := map[string]interface{}{
			"pattern":        "README",
			"case_sensitive": false,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}
	})

	t.Run("include directories", func(t *testing.T) {
		args := map[string]interface{}{
			"pattern":      "subdir",
			"include_dirs": true,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}
	})

	t.Run("with max depth", func(t *testing.T) {
		args := map[string]interface{}{
			"pattern":   "\\.go$",
			"max_depth": 1,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}
	})
}

func TestFileSearchTool_Execute_Disabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
		},
	}

	tool := NewFileSearchTool(cfg)
	ctx := context.Background()

	args := map[string]interface{}{
		"pattern": "\\.go$",
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when tool is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when tool is disabled")
	}
}
