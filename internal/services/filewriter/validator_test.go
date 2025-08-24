package filewriter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/inference-gateway/cli/config"
)

func TestPathValidator_Validate(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Sandbox: config.SandboxConfig{
				Directories: []string{tempDir},
			},
		},
	}

	validator := NewPathValidator(cfg)

	tests := []struct {
		name      string
		path      string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "empty path",
			path:      "",
			wantError: true,
			errorMsg:  "path cannot be empty",
		},
		{
			name:      "path traversal attempt",
			path:      tempDir + "/../etc/passwd",
			wantError: true,
			errorMsg:  "path traversal attempts are not allowed",
		},
		{
			name:      "null byte in path",
			path:      tempDir + "/test\x00file",
			wantError: true,
			errorMsg:  "path contains null bytes",
		},
		{
			name:      "valid path in sandbox",
			path:      filepath.Join(tempDir, "test.txt"),
			wantError: false,
		},
		{
			name:      "protected .git directory",
			path:      filepath.Join(tempDir, ".git/config"),
			wantError: true,
			errorMsg:  "path is protected",
		},
		{
			name:      "protected .env file",
			path:      filepath.Join(tempDir, ".env"),
			wantError: true,
			errorMsg:  "path is protected",
		},
		{
			name:      "protected key file",
			path:      filepath.Join(tempDir, "private.key"),
			wantError: true,
			errorMsg:  "path is protected",
		},
		{
			name:      "protected .infer directory",
			path:      filepath.Join(tempDir, ".infer/config.yaml"),
			wantError: true,
			errorMsg:  "path is protected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.path)
			validateTestResult(t, err, tt.wantError, tt.errorMsg)
		})
	}
}

func validateTestResult(t *testing.T, err error, wantError bool, errorMsg string) {
	if wantError {
		if err == nil {
			t.Errorf("Validate() expected error but got none")
			return
		}
		if errorMsg != "" && err.Error() != errorMsg && !contains(err.Error(), errorMsg) {
			t.Errorf("Validate() error = %v, want error containing %v", err, errorMsg)
		}
		return
	}

	if err != nil {
		t.Errorf("Validate() unexpected error = %v", err)
	}
}

func TestPathValidator_IsWritable(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Sandbox: config.SandboxConfig{
				Directories: []string{tempDir},
			},
		},
	}

	validator := NewPathValidator(cfg)

	testFile := filepath.Join(tempDir, "writable.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	readOnlyFile := filepath.Join(tempDir, "readonly.txt")
	if err := os.WriteFile(readOnlyFile, []byte("test"), 0444); err != nil {
		t.Fatalf("Failed to create readonly file: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "writable existing file",
			path:     testFile,
			expected: true,
		},
		{
			name:     "non-existent file in writable directory",
			path:     filepath.Join(tempDir, "newfile.txt"),
			expected: true,
		},
		{
			name:     "path outside sandbox",
			path:     "/etc/passwd",
			expected: false,
		},
		{
			name:     "protected path",
			path:     filepath.Join(tempDir, ".git/config"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.IsWritable(tt.path)
			if result != tt.expected {
				t.Errorf("IsWritable() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestPathValidator_IsInSandbox(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Sandbox: config.SandboxConfig{
				Directories: []string{tempDir},
			},
		},
	}

	validator := NewPathValidator(cfg)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "path in sandbox",
			path:     filepath.Join(tempDir, "file.txt"),
			expected: true,
		},
		{
			name:     "path outside sandbox",
			path:     "/etc/passwd",
			expected: false,
		},
		{
			name:     "sandbox directory itself",
			path:     tempDir,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.IsInSandbox(tt.path)
			if result != tt.expected {
				t.Errorf("IsInSandbox() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) > 0 &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			len(s) > len(substr) && findInString(s, substr)))
}

func findInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
