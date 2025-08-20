package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

func TestReadTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{"."},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
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

	params := def.Parameters.(map[string]interface{})
	if params["type"] != "object" {
		t.Error("Expected type to be object")
	}

	if params["additionalProperties"] != false {
		t.Error("Expected additionalProperties to be false")
	}

	properties, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected properties to be a map")
	}

	required, ok := params["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "file_path" {
		t.Error("Expected file_path to be the only required parameter")
	}

	if _, exists := properties["file_path"]; !exists {
		t.Error("file_path parameter should exist")
	}

	if limitParam, exists := properties["limit"]; exists {
		limitMap := limitParam.(map[string]interface{})
		if limitMap["minimum"] != 1 {
			t.Error("limit minimum should be 1")
		}
	}

	if offsetParam, exists := properties["offset"]; exists {
		offsetMap := offsetParam.(map[string]interface{})
		if offsetMap["minimum"] != 1 {
			t.Error("offset minimum should be 1")
		}
	}
}

func TestReadTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name          string
		toolsEnabled  bool
		readEnabled   bool
		expectedState bool
	}{
		{
			name:          "enabled when both tools and read enabled",
			toolsEnabled:  true,
			readEnabled:   true,
			expectedState: true,
		},
		{
			name:          "disabled when tools disabled",
			toolsEnabled:  false,
			readEnabled:   true,
			expectedState: false,
		},
		{
			name:          "disabled when read disabled",
			toolsEnabled:  true,
			readEnabled:   false,
			expectedState: false,
		},
		{
			name:          "disabled when both disabled",
			toolsEnabled:  false,
			readEnabled:   false,
			expectedState: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
					Read: config.ReadToolConfig{
						Enabled: tt.readEnabled,
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
	wd, _ := os.Getwd()
	parentDir := filepath.Dir(wd)
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{wd, parentDir, "/tmp", "/home/user"},
				ProtectedPaths: []string{
					".infer/",
					".git/",
					"*.env",
					"*.env.database",
				},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewReadTool(cfg)

	t.Run("basic validation tests", func(t *testing.T) {
		testBasicValidation(t, tool)
	})

	t.Run("path security tests", func(t *testing.T) {
		testPathSecurity(t, tool)
	})

	t.Run("parameter validation tests", func(t *testing.T) {
		testParameterValidation(t, tool)
	})
}

func testBasicValidation(t *testing.T, tool *ReadTool) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid absolute file path",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
			},
			wantError: false,
		},
		{
			name: "valid absolute file path with offset and limit",
			args: map[string]interface{}{
				"file_path": "/home/user/test.txt",
				"offset":    float64(5),
				"limit":     float64(100),
			},
			wantError: false,
		},
		{
			name:      "missing file_path",
			args:      map[string]interface{}{},
			wantError: true,
			errorMsg:  "file_path parameter is required and must be a string",
		},
		{
			name: "empty file_path",
			args: map[string]interface{}{
				"file_path": "",
			},
			wantError: true,
			errorMsg:  "file_path cannot be empty",
		},
		{
			name: "file_path wrong type",
			args: map[string]interface{}{
				"file_path": 123,
			},
			wantError: true,
			errorMsg:  "file_path parameter is required and must be a string",
		},
		{
			name: "relative path accepted",
			args: map[string]interface{}{
				"file_path": "relative/path.txt",
			},
			wantError: false,
		},
		{
			name: "relative path with dots accepted",
			args: map[string]interface{}{
				"file_path": "../test.txt",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if tt.wantError && tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("Expected error to contain '%s', got '%s'", tt.errorMsg, err.Error())
			}
		})
	}
}

func testPathSecurity(t *testing.T, tool *ReadTool) {
	wd, _ := os.Getwd()
	tests := []struct {
		name      string
		args      map[string]interface{}
		wantError bool
	}{
		{
			name: "excluded path",
			args: map[string]interface{}{
				"file_path": "/.infer/config.yaml",
			},
			wantError: true,
		},
		{
			name: "excluded pattern",
			args: map[string]interface{}{
				"file_path": filepath.Join(wd, ".env"),
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

func testParameterValidation(t *testing.T, tool *ReadTool) {
	tests := []struct {
		name      string
		args      map[string]interface{}
		wantError bool
		errorMsg  string
	}{
		{
			name: "invalid offset - zero",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"offset":    float64(0),
			},
			wantError: true,
			errorMsg:  "offset must be >= 1",
		},
		{
			name: "invalid offset - negative",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"offset":    float64(-1),
			},
			wantError: true,
			errorMsg:  "offset must be >= 1",
		},
		{
			name: "invalid limit - zero",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"limit":     float64(0),
			},
			wantError: true,
			errorMsg:  "limit must be >= 1",
		},
		{
			name: "invalid limit - negative",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"limit":     float64(-5),
			},
			wantError: true,
			errorMsg:  "limit must be >= 1",
		},
		{
			name: "invalid offset type",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"offset":    "not_a_number",
			},
			wantError: true,
			errorMsg:  "offset must be a number",
		},
		{
			name: "invalid limit type",
			args: map[string]interface{}{
				"file_path": "/tmp/test.txt",
				"limit":     "not_a_number",
			},
			wantError: true,
			errorMsg:  "limit must be a number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
				return
			}
			if tt.wantError && tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("Expected error to contain '%s', got '%s'", tt.errorMsg, err.Error())
			}
		})
	}
}

func TestReadTool_Execute_BasicFunctionality(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewReadTool(cfg)
	ctx := context.Background()

	t.Run("read entire file with cat -n formatting", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test.txt")
		testContent := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\n"
		err := os.WriteFile(testFile, []byte(testContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

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

		data := result.Data.(*domain.FileReadToolResult)
		lines := strings.Split(data.Content, "\n")

		expectedLines := []string{
			"     1\tLine 1",
			"     2\tLine 2",
			"     3\tLine 3",
			"     4\tLine 4",
			"     5\tLine 5",
		}

		if len(lines) != len(expectedLines) {
			t.Errorf("Expected %d lines, got %d", len(expectedLines), len(lines))
		}

		for i, expected := range expectedLines {
			if i < len(lines) && lines[i] != expected {
				t.Errorf("Line %d: expected '%s', got '%s'", i+1, expected, lines[i])
			}
		}
	})

	t.Run("test line numbering for various ranges", func(t *testing.T) {
		tests := []struct {
			lineNum  int
			expected string
		}{
			{1, "     1\t"},
			{9, "     9\t"},
			{10, "    10\t"},
			{99, "    99\t"},
			{100, "   100\t"},
			{1000, "  1000\t"},
		}

		for _, tt := range tests {
			formatted := fmt.Sprintf("%6d\t", tt.lineNum)
			if formatted != tt.expected {
				t.Errorf("Line %d: expected '%s', got '%s'", tt.lineNum, tt.expected, formatted)
			}
		}
	})
}

func TestReadTool_Execute_Paging(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewReadTool(cfg)
	ctx := context.Background()

	t.Run("read with offset and limit", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "large_test.txt")
		var lines []string
		for i := 1; i <= 20; i++ {
			lines = append(lines, fmt.Sprintf("Line %d", i))
		}
		testContent := strings.Join(lines, "\n")
		err := os.WriteFile(testFile, []byte(testContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		args := map[string]interface{}{
			"file_path": testFile,
			"offset":    float64(5),
			"limit":     float64(3),
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}

		data := result.Data.(*domain.FileReadToolResult)
		resultLines := strings.Split(data.Content, "\n")

		expectedLines := []string{
			"     5\tLine 5",
			"     6\tLine 6",
			"     7\tLine 7",
		}

		if len(resultLines) != len(expectedLines) {
			t.Errorf("Expected %d lines, got %d", len(expectedLines), len(resultLines))
		}

		for i, expected := range expectedLines {
			if i < len(resultLines) && resultLines[i] != expected {
				t.Errorf("Line %d: expected '%s', got '%s'", i+1, expected, resultLines[i])
			}
		}

		if data.StartLine != 5 {
			t.Errorf("Expected StartLine = 5, got %d", data.StartLine)
		}
		if data.EndLine != 7 {
			t.Errorf("Expected EndLine = 7, got %d", data.EndLine)
		}
	})

	t.Run("edge case - offset beyond file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "small_test.txt")
		testContent := "Line 1\nLine 2"
		err := os.WriteFile(testFile, []byte(testContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		args := map[string]interface{}{
			"file_path": testFile,
			"offset":    float64(10),
			"limit":     float64(5),
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}

		data := result.Data.(*domain.FileReadToolResult)
		if data.Content != "" {
			t.Errorf("Expected empty content, got '%s'", data.Content)
		}
	})
}

func TestReadTool_Execute_LineTruncation(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewReadTool(cfg)
	ctx := context.Background()

	t.Run("truncate long lines", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "long_lines.txt")

		longLine := strings.Repeat("a", 2500)
		testContent := fmt.Sprintf("Short line\n%s\nAnother short line", longLine)

		err := os.WriteFile(testFile, []byte(testContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		args := map[string]interface{}{
			"file_path": testFile,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}

		data := result.Data.(*domain.FileReadToolResult)
		lines := strings.Split(data.Content, "\n")

		if len(lines) < 2 {
			t.Fatal("Expected at least 2 lines")
		}

		line2Content := strings.TrimPrefix(lines[1], "     2\t")
		if len(line2Content) != MaxLineLength {
			t.Errorf("Expected line 2 content to be %d chars, got %d", MaxLineLength, len(line2Content))
		}

		expectedTruncated := strings.Repeat("a", MaxLineLength)
		if line2Content != expectedTruncated {
			t.Error("Line 2 content does not match expected truncated content")
		}
	})
}

func TestReadTool_Execute_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewReadTool(cfg)
	ctx := context.Background()

	t.Run("empty file returns reminder", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "empty.txt")
		err := os.WriteFile(testFile, []byte(""), 0644)
		if err != nil {
			t.Fatalf("Failed to create empty test file: %v", err)
		}

		args := map[string]interface{}{
			"file_path": testFile,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}

		data := result.Data.(*domain.FileReadToolResult)
		if data.Content != EmptyFileReminder {
			t.Errorf("Expected empty file reminder, got '%s'", data.Content)
		}

		if data.Error != ErrorFileEmpty {
			t.Errorf("Expected error to be %s, got '%s'", ErrorFileEmpty, data.Error)
		}
	})
}

func TestReadTool_Execute_ErrorCases(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewReadTool(cfg)
	ctx := context.Background()

	tests := []struct {
		name          string
		args          map[string]interface{}
		expectedError string
	}{
		{
			name: "relative path",
			args: map[string]interface{}{
				"file_path": filepath.Join(tmpDir, "relative/path.txt"),
			},
			expectedError: ErrorNotFound,
		},
		{
			name: "nonexistent file",
			args: map[string]interface{}{
				"file_path": filepath.Join(tmpDir, "nonexistent.txt"),
			},
			expectedError: ErrorNotFound,
		},
		{
			name: "directory instead of file",
			args: map[string]interface{}{
				"file_path": tmpDir,
			},
			expectedError: "is a directory, not a file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(ctx, tt.args)
			if err != nil {
				t.Fatalf("Execute() returned unexpected error: %v", err)
			}

			if result.Success {
				t.Error("Expected unsuccessful execution")
			}

			if !strings.Contains(result.Error, tt.expectedError) {
				t.Errorf("Expected error to contain '%s', got '%s'", tt.expectedError, result.Error)
			}
		})
	}
}

func TestReadTool_Execute_BinaryFileDetection(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewReadTool(cfg)
	ctx := context.Background()

	t.Run("binary file detection", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "binary.bin")

		binaryContent := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
		err := os.WriteFile(testFile, binaryContent, 0644)
		if err != nil {
			t.Fatalf("Failed to create binary test file: %v", err)
		}

		args := map[string]interface{}{
			"file_path": testFile,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if result.Success {
			t.Error("Expected unsuccessful execution for binary file")
		}

		if !strings.Contains(result.Error, ErrorUnreadableBinary) {
			t.Errorf("Expected error to contain '%s', got '%s'", ErrorUnreadableBinary, result.Error)
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
		"file_path": "/tmp/test.txt",
	}

	result, err := tool.Execute(ctx, args)
	if err == nil {
		t.Fatal("Expected error when tool is disabled")
	}

	if result != nil {
		t.Error("Expected nil result when tool is disabled")
	}
}

func TestReadTool_Execute_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Sandbox: config.SandboxConfig{
				Directories: []string{tmpDir},
			},
			Read: config.ReadToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewReadTool(cfg)
	ctx := context.Background()

	t.Run("defaults applied when not specified", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "defaults_test.txt")

		var lines []string
		for i := 1; i <= 2500; i++ {
			lines = append(lines, fmt.Sprintf("Line %d", i))
		}
		testContent := strings.Join(lines, "\n")
		err := os.WriteFile(testFile, []byte(testContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		args := map[string]interface{}{
			"file_path": testFile,
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute() failed: %v", err)
		}

		if !result.Success {
			t.Error("Expected successful execution")
		}

		data := result.Data.(*domain.FileReadToolResult)
		resultLines := strings.Split(data.Content, "\n")

		if len(resultLines) != DefaultLimit {
			t.Errorf("Expected %d lines (default limit), got %d", DefaultLimit, len(resultLines))
		}

		if !strings.HasPrefix(resultLines[0], "     1\t") {
			t.Error("Expected first line to start with line number 1")
		}

		if data.StartLine != DefaultOffset {
			t.Errorf("Expected StartLine = %d, got %d", DefaultOffset, data.StartLine)
		}

		expectedEndLine := DefaultOffset + DefaultLimit - 1
		if data.EndLine != expectedEndLine {
			t.Errorf("Expected EndLine = %d, got %d", expectedEndLine, data.EndLine)
		}
	})
}
