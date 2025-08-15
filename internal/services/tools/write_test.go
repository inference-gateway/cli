package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

func TestWriteTool_Definition(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteTool(cfg)

	def := tool.Definition()
	if def.Name != "Write" {
		t.Errorf("Expected tool name 'Write', got '%s'", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	params, ok := def.Parameters.(map[string]interface{})
	if !ok {
		t.Fatal("Parameters should be a map")
	}

	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Properties should be a map")
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("Required should be a string slice")
	}

	if len(required) != 2 {
		t.Errorf("Expected 2 required parameters, got %d", len(required))
	}

	expectedFields := []string{"file_path", "content", "create_dirs", "overwrite", "format"}
	for _, field := range expectedFields {
		if _, exists := props[field]; !exists {
			t.Errorf("Expected field '%s' in properties", field)
		}
	}
}

func TestWriteTool_IsEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteTool(cfg)

	if !tool.IsEnabled() {
		t.Error("Write tool should be enabled by default")
	}

	cfg.Tools.Enabled = false
	tool = NewWriteTool(cfg)
	if tool.IsEnabled() {
		t.Error("Write tool should be disabled when tools are globally disabled")
	}

	cfg.Tools.Enabled = true
	cfg.Tools.Write.Enabled = false
	tool = NewWriteTool(cfg)
	if tool.IsEnabled() {
		t.Error("Write tool should be disabled when write tool is specifically disabled")
	}
}

func TestWriteTool_Validate(t *testing.T) {
	cfg := config.DefaultConfig()
	tool := NewWriteTool(cfg)

	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid basic arguments",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   "hello world",
			},
			wantErr: false,
		},
		{
			name: "valid with all optional arguments",
			args: map[string]interface{}{
				"file_path":   "test.txt",
				"content":     "hello world",
				"create_dirs": true,
				"overwrite":   false,
				"format":      "json",
			},
			wantErr: false,
		},
		{
			name:    "missing file_path",
			args:    map[string]interface{}{"content": "hello"},
			wantErr: true,
			errMsg:  "file_path parameter is required and must be a string",
		},
		{
			name:    "missing content",
			args:    map[string]interface{}{"file_path": "test.txt"},
			wantErr: true,
			errMsg:  "content parameter is required and must be a string",
		},
		{
			name: "empty file_path",
			args: map[string]interface{}{
				"file_path": "",
				"content":   "hello",
			},
			wantErr: true,
			errMsg:  "file_path cannot be empty",
		},
		{
			name: "invalid file_path type",
			args: map[string]interface{}{
				"file_path": 123,
				"content":   "hello",
			},
			wantErr: true,
			errMsg:  "file_path parameter is required and must be a string",
		},
		{
			name: "invalid content type",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   123,
			},
			wantErr: true,
			errMsg:  "content parameter is required and must be a string",
		},
		{
			name: "invalid create_dirs type",
			args: map[string]interface{}{
				"file_path":   "test.txt",
				"content":     "hello",
				"create_dirs": "true",
			},
			wantErr: true,
			errMsg:  "create_dirs parameter must be a boolean",
		},
		{
			name: "invalid overwrite type",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   "hello",
				"overwrite": "false",
			},
			wantErr: true,
			errMsg:  "overwrite parameter must be a boolean",
		},
		{
			name: "invalid format value",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   "hello",
				"format":    "xml",
			},
			wantErr: true,
			errMsg:  "format must be 'text' or 'json'",
		},
		{
			name: "invalid format type",
			args: map[string]interface{}{
				"file_path": "test.txt",
				"content":   "hello",
				"format":    123,
			},
			wantErr: true,
			errMsg:  "format parameter must be a string",
		},
		{
			name: "excluded path",
			args: map[string]interface{}{
				"file_path": ".infer/test.txt",
				"content":   "hello",
			},
			wantErr: true,
			errMsg:  "access to path '.infer/test.txt' is excluded for security",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Expected error '%s', got '%s'", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestWriteTool_ValidateDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.Enabled = false
	tool := NewWriteTool(cfg)

	args := map[string]interface{}{
		"file_path": "test.txt",
		"content":   "hello",
	}

	err := tool.Validate(args)
	if err == nil {
		t.Error("Expected error when tools are disabled")
	}
	if err.Error() != "write tool is not enabled" {
		t.Errorf("Expected 'write tool is not enabled', got '%s'", err.Error())
	}
}

func TestWriteTool_Execute(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	tool := NewWriteTool(cfg)
	ctx := context.Background()

	t.Run("successful write to new file", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "test1.txt")
		content := "Hello, World!"

		args := map[string]interface{}{
			"file_path": filePath,
			"content":   content,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success=true, got %v", result.Success)
		}

		if result.Error != "" {
			t.Errorf("Expected no error, got: %s", result.Error)
		}

		data, ok := result.Data.(*domain.FileWriteToolResult)
		if !ok {
			t.Fatalf("Expected FileWriteToolResult, got %T", result.Data)
		}

		if data.FilePath != filePath {
			t.Errorf("Expected file_path='%s', got '%s'", filePath, data.FilePath)
		}

		if data.BytesWriten != int64(len(content)) {
			t.Errorf("Expected bytes_written=%d, got %d", len(content), data.BytesWriten)
		}

		if !data.Created {
			t.Error("Expected created=true")
		}

		if data.Overwritten {
			t.Error("Expected overwritten=false")
		}

		// Verify file was actually written
		writtenContent, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Failed to read written file: %v", err)
		}

		if string(writtenContent) != content {
			t.Errorf("Expected content='%s', got '%s'", content, string(writtenContent))
		}
	})

	t.Run("successful write with directory creation", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "subdir", "test2.txt")
		content := "Hello, Directory!"

		args := map[string]interface{}{
			"file_path":   filePath,
			"content":     content,
			"create_dirs": true,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success=true, got %v", result.Success)
		}

		data, ok := result.Data.(*domain.FileWriteToolResult)
		if !ok {
			t.Fatalf("Expected FileWriteToolResult, got %T", result.Data)
		}

		if !data.DirsCreated {
			t.Error("Expected dirs_created=true")
		}

		// Verify file was actually written
		writtenContent, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Failed to read written file: %v", err)
		}

		if string(writtenContent) != content {
			t.Errorf("Expected content='%s', got '%s'", content, string(writtenContent))
		}
	})

	t.Run("successful overwrite existing file", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "test3.txt")
		originalContent := "Original content"
		newContent := "New content"

		// First, create a file
		if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
			t.Fatalf("Failed to create initial file: %v", err)
		}

		args := map[string]interface{}{
			"file_path": filePath,
			"content":   newContent,
			"overwrite": true,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		if !result.Success {
			t.Errorf("Expected success=true, got %v", result.Success)
		}

		data, ok := result.Data.(*domain.FileWriteToolResult)
		if !ok {
			t.Fatalf("Expected FileWriteToolResult, got %T", result.Data)
		}

		if data.Created {
			t.Error("Expected created=false")
		}

		if !data.Overwritten {
			t.Error("Expected overwritten=true")
		}

		// Verify file was actually overwritten
		writtenContent, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Failed to read written file: %v", err)
		}

		if string(writtenContent) != newContent {
			t.Errorf("Expected content='%s', got '%s'", newContent, string(writtenContent))
		}
	})

	t.Run("fail when overwrite is false and file exists", func(t *testing.T) {
		filePath := filepath.Join(tempDir, "test4.txt")
		originalContent := "Original content"
		newContent := "New content"

		// First, create a file
		if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
			t.Fatalf("Failed to create initial file: %v", err)
		}

		args := map[string]interface{}{
			"file_path": filePath,
			"content":   newContent,
			"overwrite": false,
		}

		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Error("Expected error when overwrite=false and file exists")
		}

		if err.Error() != "file "+filePath+" already exists and overwrite is false" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}

		// Verify original file was not modified
		writtenContent, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Failed to read original file: %v", err)
		}

		if string(writtenContent) != originalContent {
			t.Error("Original file should not have been modified")
		}
	})

	t.Run("fail with invalid arguments", func(t *testing.T) {
		args := map[string]interface{}{
			"file_path": 123,
			"content":   "hello",
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute should not return error for validation failures: %v", err)
		}

		if result.Success {
			t.Error("Expected success=false")
		}

		if result.Error != "file_path parameter is required and must be a string" {
			t.Errorf("Expected validation error, got: %s", result.Error)
		}
	})

	t.Run("fail when tools are disabled", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Tools.Enabled = false
		disabledTool := NewWriteTool(cfg)

		args := map[string]interface{}{
			"file_path": "test.txt",
			"content":   "hello",
		}

		_, err := disabledTool.Execute(ctx, args)
		if err == nil {
			t.Error("Expected error when tools are disabled")
		}
	})
}

func TestWriteTool_PathSecurity(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.ExcludePaths = []string{".infer/", "*.secret", "/etc/*"}
	tool := NewWriteTool(cfg)

	tests := []struct {
		name     string
		path     string
		allowed  bool
		errorMsg string
	}{
		{
			name:    "allowed path",
			path:    "test.txt",
			allowed: true,
		},
		{
			name:    "allowed subdirectory",
			path:    "subdir/test.txt",
			allowed: true,
		},
		{
			name:     "excluded .infer directory",
			path:     ".infer/config.yaml",
			allowed:  false,
			errorMsg: "access to path '.infer/config.yaml' is excluded for security",
		},
		{
			name:     "excluded pattern *.secret",
			path:     "database.secret",
			allowed:  false,
			errorMsg: "access to path 'database.secret' is excluded for security",
		},
		{
			name:     "excluded pattern /etc/*",
			path:     "/etc/passwd",
			allowed:  false,
			errorMsg: "access to path '/etc/passwd' is excluded for security",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{
				"file_path": tt.path,
				"content":   "test content",
			}

			err := tool.Validate(args)
			if tt.allowed {
				if err != nil {
					t.Errorf("Path should be allowed: %v", err)
				}
			} else {
				if err == nil {
					t.Error("Path should be blocked")
				} else if tt.errorMsg != "" && err.Error() != tt.errorMsg {
					t.Errorf("Expected error '%s', got '%s'", tt.errorMsg, err.Error())
				}
			}
		})
	}
}
