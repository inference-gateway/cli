package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileServiceImpl_ListProjectFiles(t *testing.T) {
	tmpDir := t.TempDir()

	testFiles := []struct {
		path    string
		content string
		isDir   bool
	}{
		{"README.md", "# Test", false},
		{"main.go", "package main", false},
		{".gitignore", "*.log", false},
		{".git/config", "[core]", false},
		{".github/workflows/ci.yml", "name: CI", false},
		{".infer/chat_export.md", "## Summary\nTest export", false},
		{".infer/config.yaml", "gateway:", false},
		{".infer/debug.log", "debug info", false},
		{"src/file.go", "package src", false},
		{"node_modules/package/index.js", "module.exports = {}", false},
		{"large_file.txt", string(make([]byte, 200*1024)), false}, // 200KB file
		{".hidden_file.txt", "hidden", false},
	}

	for _, tf := range testFiles {
		fullPath := filepath.Join(tmpDir, tf.path)
		dir := filepath.Dir(fullPath)

		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create dir %s: %v", dir, err)
		}

		if !tf.isDir {
			if err := os.WriteFile(fullPath, []byte(tf.content), 0644); err != nil {
				t.Fatalf("Failed to create file %s: %v", fullPath, err)
			}
		}
	}

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Errorf("Failed to restore directory: %v", err)
		}
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	fileService := NewFileService()
	files, err := fileService.ListProjectFiles()
	if err != nil {
		t.Fatalf("ListProjectFiles failed: %v", err)
	}

	expectedFiles := []string{
		"README.md",
		"main.go",
		".infer/chat_export.md",
		"src/file.go",
	}

	for _, expected := range expectedFiles {
		found := false
		for _, file := range files {
			if file == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected file %s not found in results", expected)
		}
	}

	excludedFiles := []string{
		".gitignore",                    // Hidden files are excluded
		".git/config",                   // .git directory is excluded
		".github/workflows/ci.yml",      // .github directory is excluded
		".infer/config.yaml",            // Only .md files from .infer are included
		".infer/debug.log",              // Only .md files from .infer are included
		"node_modules/package/index.js", // node_modules is excluded
		"large_file.txt",                // Files over 100KB are excluded
		".hidden_file.txt",              // Hidden files are excluded
	}

	for _, excluded := range excludedFiles {
		for _, file := range files {
			if file == excluded {
				t.Errorf("Excluded file %s found in results", excluded)
			}
		}
	}
}

func TestFileServiceImpl_ValidateFile(t *testing.T) {
	tmpDir := t.TempDir()

	smallFile := filepath.Join(tmpDir, "small.txt")
	largeFile := filepath.Join(tmpDir, "large.txt")
	markdownFile := filepath.Join(tmpDir, "large.md")

	if err := os.WriteFile(smallFile, []byte("small content"), 0644); err != nil {
		t.Fatalf("Failed to create small file: %v", err)
	}

	largeContent := make([]byte, 60*1024) // 60KB
	if err := os.WriteFile(largeFile, largeContent, 0644); err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	markdownContent := make([]byte, 100*1024) // 100KB
	if err := os.WriteFile(markdownFile, markdownContent, 0644); err != nil {
		t.Fatalf("Failed to create markdown file: %v", err)
	}

	fileService := NewFileService()

	tests := []struct {
		name      string
		file      string
		shouldErr bool
	}{
		{"empty path", "", true},
		{"non-existent file", filepath.Join(tmpDir, "nonexistent.txt"), true},
		{"small file", smallFile, false},
		{"large text file", largeFile, true},
		{"large markdown file", markdownFile, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fileService.ValidateFile(tt.file)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error for %s, but got none", tt.name)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Expected no error for %s, but got: %v", tt.name, err)
			}
		})
	}
}

func TestFileServiceImpl_ReadFile(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "test.txt")
	content := "Hello, World!\nThis is a test file."

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fileService := NewFileService()

	result, err := fileService.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if result != content {
		t.Errorf("Expected content %q, got %q", content, result)
	}
}

func TestFileServiceImpl_ReadFileLines(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "lines.txt")
	content := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fileService := NewFileService()

	tests := []struct {
		name      string
		startLine int
		endLine   int
		expected  string
		shouldErr bool
	}{
		{"first two lines", 1, 2, "Line 1\nLine 2", false},
		{"middle lines", 2, 4, "Line 2\nLine 3\nLine 4", false},
		{"invalid start line", 0, 2, "", true},
		{"start line too high", 10, 12, "", true},
		{"end line beyond file", 3, 10, "Line 3\nLine 4\nLine 5", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := fileService.ReadFileLines(testFile, tt.startLine, tt.endLine)

			if tt.shouldErr {
				if err == nil {
					t.Errorf("Expected error for %s, but got none", tt.name)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.name, err)
				return
			}

			if result != tt.expected {
				t.Errorf("For %s, expected %q, got %q", tt.name, tt.expected, result)
			}
		})
	}
}

func TestFileServiceImpl_GetFileInfo(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "info.txt")
	content := "File info test"

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	fileService := NewFileService()

	info, err := fileService.GetFileInfo(testFile)
	if err != nil {
		t.Fatalf("GetFileInfo failed: %v", err)
	}

	if info.Path != testFile {
		t.Errorf("Expected path %s, got %s", testFile, info.Path)
	}

	if info.Size != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), info.Size)
	}

	if info.IsDir {
		t.Error("Expected IsDir to be false for a file")
	}
}

func TestFileServiceImpl_InferDirectoryHandling(t *testing.T) {
	tmpDir := t.TempDir()

	inferDir := filepath.Join(tmpDir, ".infer")
	if err := os.MkdirAll(inferDir, 0755); err != nil {
		t.Fatalf("Failed to create .infer directory: %v", err)
	}

	testFiles := map[string]string{
		"chat_export.md": "## Summary\nTest export",
		"config.yaml":    "gateway: http://localhost",
		"debug.log":      "debug information",
		"history":        "command history",
	}

	for name, content := range testFiles {
		path := filepath.Join(inferDir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", path, err)
		}
	}

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Errorf("Failed to restore directory: %v", err)
		}
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	fileService := NewFileService()
	files, err := fileService.ListProjectFiles()
	if err != nil {
		t.Fatalf("ListProjectFiles failed: %v", err)
	}

	expectedFound := []string{".infer/chat_export.md"}
	expectedNotFound := []string{
		".infer/config.yaml",
		".infer/debug.log",
		".infer/history",
	}

	for _, expected := range expectedFound {
		found := false
		for _, file := range files {
			if file == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected .infer file %s not found", expected)
		}
	}

	for _, notExpected := range expectedNotFound {
		for _, file := range files {
			if file == notExpected {
				t.Errorf("Unexpected .infer file %s found", notExpected)
			}
		}
	}
}
