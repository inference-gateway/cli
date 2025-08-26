package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

func TestNewTreeTool(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Tree: config.TreeToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTreeTool(cfg)
	if tool == nil {
		t.Fatal("NewTreeTool returned nil")
	}

	if !tool.IsEnabled() {
		t.Error("Tree tool should be enabled")
	}
}

func TestTreeTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Tree: config.TreeToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTreeTool(cfg)
	def := tool.Definition()

	if def.Name != "Tree" {
		t.Errorf("Expected tool name 'Tree', got '%s'", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestTreeTool_Validate(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{"."},
				ProtectedPaths: []string{
					".infer/",
					".git/",
				},
			},
			Tree: config.TreeToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTreeTool(cfg)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{
			name:    "valid empty args",
			args:    map[string]any{},
			wantErr: false,
		},
		{
			name: "valid path",
			args: map[string]any{
				"path": ".",
			},
			wantErr: false,
		},
		{
			name: "valid max_depth",
			args: map[string]any{
				"max_depth": float64(3),
			},
			wantErr: false,
		},
		{
			name: "invalid max_depth negative",
			args: map[string]any{
				"max_depth": float64(-1),
			},
			wantErr: true,
		},
		{
			name: "invalid max_depth zero",
			args: map[string]any{
				"max_depth": float64(0),
			},
			wantErr: true,
		},
		{
			name: "invalid max_depth type",
			args: map[string]any{
				"max_depth": "invalid",
			},
			wantErr: true,
		},
		{
			name: "valid show_hidden",
			args: map[string]any{
				"show_hidden": true,
			},
			wantErr: false,
		},
		{
			name: "invalid show_hidden type",
			args: map[string]any{
				"show_hidden": "not_a_bool",
			},
			wantErr: true,
		},
		{
			name: "valid format text",
			args: map[string]any{
				"format": "text",
			},
			wantErr: false,
		},
		{
			name: "valid format json",
			args: map[string]any{
				"format": "json",
			},
			wantErr: false,
		},
		{
			name: "invalid format",
			args: map[string]any{
				"format": "xml",
			},
			wantErr: true,
		},
		{
			name: "excluded path",
			args: map[string]any{
				"path": ".infer/test",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTreeTool_ValidateToolDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
			Tree: config.TreeToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTreeTool(cfg)
	err := tool.Validate(map[string]any{})
	if err == nil {
		t.Error("Expected error when tools are disabled")
	}
}

func setupTestDirectory(t *testing.T) string {
	tempDir := t.TempDir()

	testFiles := []string{
		"file1.txt",
		"file2.log",
		"dir1/file3.txt",
		"dir1/subdir/file4.txt",
		"dir2/file5.txt",
		".hidden/file6.txt",
	}

	for _, file := range testFiles {
		fullPath := filepath.Join(tempDir, file)
		dir := filepath.Dir(fullPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
		if err := os.WriteFile(fullPath, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", fullPath, err)
		}
	}

	return tempDir
}

func createTestTreeTool(tempDir string) *TreeTool {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tempDir},
			},
			Tree: config.TreeToolConfig{
				Enabled: true,
			},
		},
	}
	return NewTreeTool(cfg)
}

func TestTreeTool_ExecuteBasic(t *testing.T) {
	tempDir := setupTestDirectory(t)
	tool := createTestTreeTool(tempDir)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"path": tempDir,
	})

	if err != nil {
		t.Errorf("Execute() error = %v", err)
		return
	}

	if !result.Success {
		t.Error("Expected successful execution")
	}

	if result.Data == nil {
		t.Error("Expected result data")
		return
	}

	treeResult, ok := result.Data.(*domain.TreeToolResult)
	if !ok {
		t.Error("Expected TreeToolResult")
		return
	}

	if treeResult.Path != tempDir {
		t.Errorf("Expected path %s, got %s", tempDir, treeResult.Path)
	}

	if treeResult.Output == "" {
		t.Error("Expected non-empty output")
	}
}

func TestTreeTool_ExecuteWithMaxDepth(t *testing.T) {
	tempDir := setupTestDirectory(t)
	tool := createTestTreeTool(tempDir)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"path":      tempDir,
		"max_depth": float64(1),
	})

	if err != nil {
		t.Errorf("Execute() error = %v", err)
		return
	}

	if !result.Success {
		t.Error("Expected successful execution")
	}

	treeResult, ok := result.Data.(*domain.TreeToolResult)
	if !ok {
		t.Error("Expected TreeToolResult")
		return
	}

	if treeResult.MaxDepth != 1 {
		t.Errorf("Expected max_depth 1, got %d", treeResult.MaxDepth)
	}
}


func TestTreeTool_ExecuteWithShowHidden(t *testing.T) {
	tempDir := setupTestDirectory(t)
	tool := createTestTreeTool(tempDir)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"path":        tempDir,
		"show_hidden": true,
	})

	if err != nil {
		t.Errorf("Execute() error = %v", err)
		return
	}

	if !result.Success {
		t.Error("Expected successful execution")
	}

	treeResult, ok := result.Data.(*domain.TreeToolResult)
	if !ok {
		t.Error("Expected TreeToolResult")
		return
	}

	if !treeResult.ShowHidden {
		t.Error("Expected show_hidden to be true")
	}
}

func TestTreeTool_ExecuteWithJSONFormat(t *testing.T) {
	tempDir := setupTestDirectory(t)
	tool := createTestTreeTool(tempDir)
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]any{
		"path":   tempDir,
		"format": "json",
	})

	if err != nil {
		t.Errorf("Execute() error = %v", err)
		return
	}

	if !result.Success {
		t.Error("Expected successful execution")
	}

	treeResult, ok := result.Data.(*domain.TreeToolResult)
	if !ok {
		t.Error("Expected TreeToolResult")
		return
	}

	if treeResult.Format != "json" {
		t.Errorf("Expected format json, got %s", treeResult.Format)
	}

	if !strings.Contains(treeResult.Output, "tree") {
		t.Errorf("JSON output should contain tree data, got: %s", treeResult.Output)
	}
}

func TestTreeTool_ExecuteErrors(t *testing.T) {
	tempDir := setupTestDirectory(t)
	tool := createTestTreeTool(tempDir)
	ctx := context.Background()

	tests := []struct {
		name string
		args map[string]any
	}{
		{
			name: "nonexistent path",
			args: map[string]any{
				"path": "/nonexistent/path",
			},
		},
		{
			name: "file instead of directory",
			args: map[string]any{
				"path": filepath.Join(tempDir, "file1.txt"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Execute(ctx, tt.args)
			if err == nil {
				t.Error("Expected error but got none")
			}
		})
	}
}

func TestTreeTool_ExecuteToolDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
			Tree: config.TreeToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTreeTool(cfg)
	ctx := context.Background()

	_, err := tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Error("Expected error when tools are disabled")
	}
}

func TestTreeTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name           string
		toolsEnabled   bool
		treeEnabled    bool
		expectedResult bool
	}{
		{
			name:           "both enabled",
			toolsEnabled:   true,
			treeEnabled:    true,
			expectedResult: true,
		},
		{
			name:           "tools disabled",
			toolsEnabled:   false,
			treeEnabled:    true,
			expectedResult: false,
		},
		{
			name:           "tree disabled",
			toolsEnabled:   true,
			treeEnabled:    false,
			expectedResult: false,
		},
		{
			name:           "both disabled",
			toolsEnabled:   false,
			treeEnabled:    false,
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
					Tree: config.TreeToolConfig{
						Enabled: tt.treeEnabled,
					},
				},
			}

			tool := NewTreeTool(cfg)
			if tool.IsEnabled() != tt.expectedResult {
				t.Errorf("IsEnabled() = %v, want %v", tool.IsEnabled(), tt.expectedResult)
			}
		})
	}
}

func TestTreeTool_ShouldExclude(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Tree: config.TreeToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTreeTool(cfg)

	tests := []struct {
		name             string
		filename         string
		fullPath         string
		respectGitignore bool
		expected         bool
	}{
		{
			name:             "regular file with gitignore respect",
			filename:         "test.txt",
			fullPath:         "/test/path/test.txt",
			respectGitignore: true,
			expected:         false,
		},
		{
			name:             "git file should be excluded when respecting gitignore",
			filename:         "config",
			fullPath:         "/test/.git/config",
			respectGitignore: true,
			expected:         true,
		},
		{
			name:             "git file should not be excluded when not respecting gitignore",
			filename:         "config",
			fullPath:         "/test/.git/config",
			respectGitignore: false,
			expected:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.shouldExclude(tt.fullPath, tt.filename, tt.respectGitignore)
			if result != tt.expected {
				t.Errorf("shouldExclude(%s, %s, %v) = %v, want %v", tt.fullPath, tt.filename, tt.respectGitignore, result, tt.expected)
			}
		})
	}
}

func TestTreeTool_RespectGitignore(t *testing.T) {
	tests := []struct {
		name             string
		respectGitignore bool
		expectGitignored bool
	}{
		{
			name:             "with respect_gitignore=true should exclude gitignored files",
			respectGitignore: true,
			expectGitignored: true,
		},
		{
			name:             "with respect_gitignore=false should include gitignored files",
			respectGitignore: false,
			expectGitignored: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: true,
					Tree: config.TreeToolConfig{
						Enabled: true,
					},
				},
			}

			tool := NewTreeTool(cfg)

			fullPath := "/test/.git/config"
			fileName := "config"

			result := tool.shouldExclude(fullPath, fileName, tt.respectGitignore)

			if tt.expectGitignored && !result {
				t.Errorf("Expected gitignored file to be excluded when respectGitignore=true, but it wasn't")
			} else if !tt.expectGitignored && result {
				t.Errorf("Expected gitignored file to be included when respectGitignore=false, but it was excluded")
			}
		})
	}
}

func TestTreeTool_ValidatePath(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(tempFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tempDir},
			},
			Tree: config.TreeToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTreeTool(cfg)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid directory",
			path:    tempDir,
			wantErr: false,
		},
		{
			name:    "file instead of directory",
			path:    tempFile,
			wantErr: true,
		},
		{
			name:    "nonexistent path",
			path:    "/nonexistent",
			wantErr: true,
		},
		{
			name:    "excluded path",
			path:    ".infer/test",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
