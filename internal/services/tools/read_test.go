package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestReadTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}

	tool := NewReadTool(cfg)
	def := tool.Definition()

	if def.Name != "Read" {
		t.Errorf("Expected tool name 'Read', got %s", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestReadTool_IsEnabled(t *testing.T) {
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
					Read: config.ReadToolConfig{
						Enabled: true,
					},
				},
			}

			tool := NewReadTool(cfg)
			if tool.IsEnabled() != tt.expectedState {
				t.Errorf("Expected IsEnabled() = %v, got %v", tt.expectedState, tool.IsEnabled())
			}
		})
	}
}

func TestReadTool_Validate(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			ExcludePaths: []string{
				".infer/",
				"*.secret",
			},
		},
	}

	tool := NewReadTool(cfg)

	tests := []struct {
		name      string
		args      map[string]interface{}
		wantError bool
	}{
		{
			name: "valid file path",
			args: map[string]interface{}{
				"file_path": "test.txt",
			},
			wantError: false,
		},
		{
			name: "valid file path with line range",
			args: map[string]interface{}{
				"file_path":  "test.txt",
				"start_line": 1,
				"end_line":   10,
			},
			wantError: false,
		},
		{
			name: "valid file path with format",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"format":    "text",
			},
			wantError: false,
		},
		{
			name:      "missing file_path",
			args:      map[string]interface{}{},
			wantError: true,
		},
		{
			name: "empty file_path",
			args: map[string]interface{}{
				"file_path": "",
			},
			wantError: true,
		},
		{
			name: "file_path wrong type",
			args: map[string]interface{}{
				"file_path": 123,
			},
			wantError: true,
		},
		{
			name: "excluded path",
			args: map[string]interface{}{
				"file_path": ".infer/config.yaml",
			},
			wantError: true,
		},
		{
			name: "excluded pattern",
			args: map[string]interface{}{
				"file_path": "database.secret",
			},
			wantError: true,
		},
		{
			name: "invalid line range - start > end",
			args: map[string]interface{}{
				"file_path":  "test.txt",
				"start_line": 10,
				"end_line":   5,
			},
			wantError: true,
		},
		{
			name: "invalid line range - zero start",
			args: map[string]interface{}{
				"file_path":  "test.txt",
				"start_line": 0,
			},
			wantError: true,
		},
		{
			name: "invalid line range - negative end",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"end_line":  -1,
			},
			wantError: true,
		},
		{
			name: "invalid format",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"format":    "invalid",
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

func TestReadTool_Execute(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
		},
	}

	tool := NewReadTool(cfg)
	ctx := context.Background()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\n"
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	t.Run("read entire file", func(t *testing.T) {
		args := map[string]interface{}{
			"file_path": testFile,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if result == nil {
			t.Fatal("Expected non-nil result")
		}

		if result.ToolName != "Read" {
			t.Errorf("Expected tool name 'Read', got %s", result.ToolName)
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}
	})

	t.Run("read with line range", func(t *testing.T) {
		args := map[string]interface{}{
			"file_path":  testFile,
			"start_line": 2,
			"end_line":   4,
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

	t.Run("file not found", func(t *testing.T) {
		args := map[string]interface{}{
			"file_path": "nonexistent.txt",
		}

		result, err := tool.Execute(ctx, args)
		if err == nil {
			t.Fatal("Expected error for non-existent file")
		}

		if result != nil && result.Success {
			t.Error("Expected unsuccessful execution for non-existent file")
		}
	})
}

func TestReadTool_Execute_Disabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
		},
	}

	tool := NewReadTool(cfg)
	ctx := context.Background()

	args := map[string]interface{}{
		"file_path": "test.txt",
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when tool is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when tool is disabled")
	}
}
