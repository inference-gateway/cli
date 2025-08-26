package tools

import (
	"testing"

	"github.com/inference-gateway/cli/internal/domain/filewriter"
)

type writeParamsTestCase struct {
	name      string
	params    map[string]interface{}
	expected  *WriteParams
	wantError bool
	errorMsg  string
}

func getWriteParamsTestCases() []writeParamsTestCase {
	return []writeParamsTestCase{
		{
			name: "valid basic params",
			params: map[string]interface{}{
				"file_path": "/test/file.txt",
				"content":   "test content",
			},
			expected: &WriteParams{
				FilePath: "/test/file.txt",
				Content:  "test content",
			},
		},
		{
			name: "missing file_path",
			params: map[string]interface{}{
				"content": "test content",
			},
			wantError: true,
			errorMsg:  "missing required parameter: file_path",
		},
		{
			name: "missing content",
			params: map[string]interface{}{
				"file_path": "/test/file.txt",
			},
			wantError: true,
			errorMsg:  "missing required parameter: content",
		},
		{
			name: "empty file_path",
			params: map[string]interface{}{
				"file_path": "",
				"content":   "test content",
			},
			wantError: true,
			errorMsg:  "parameter file_path cannot be empty",
		},
		{
			name: "invalid file_path type",
			params: map[string]interface{}{
				"file_path": 123,
				"content":   "test content",
			},
			wantError: true,
			errorMsg:  "parameter file_path must be a string",
		},
	}
}

func validateWriteParamsResult(t *testing.T, result, expected *WriteParams) {
	if result.FilePath != expected.FilePath {
		t.Errorf("FilePath = %v, want %v", result.FilePath, expected.FilePath)
	}
	if result.Content != expected.Content {
		t.Errorf("Content = %v, want %v", result.Content, expected.Content)
	}
}

func runWriteParamsTestCase(t *testing.T, extractor *ParameterExtractor, tt writeParamsTestCase) {
	result, err := extractor.ExtractWriteParams(tt.params)

	if tt.wantError {
		if err == nil {
			t.Errorf("ExtractWriteParams() expected error but got none")
			return
		}
		if tt.errorMsg != "" && !containsSubstring(err.Error(), tt.errorMsg) {
			t.Errorf("ExtractWriteParams() error = %v, want error containing %v", err, tt.errorMsg)
		}
		return
	}

	if err != nil {
		t.Errorf("ExtractWriteParams() unexpected error = %v", err)
		return
	}

	if result == nil {
		t.Error("ExtractWriteParams() returned nil result")
		return
	}

	validateWriteParamsResult(t, result, tt.expected)
}

func TestParameterExtractor_ExtractWriteParams(t *testing.T) {
	extractor := NewParameterExtractor()
	tests := getWriteParamsTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runWriteParamsTestCase(t, extractor, tt)
		})
	}
}

func TestParameterExtractor_ToWriteRequest(t *testing.T) {
	extractor := NewParameterExtractor()

	params := &WriteParams{
		FilePath: "/test/file.txt",
		Content:  "test content",
	}

	result := extractor.ToWriteRequest(params)

	expected := filewriter.WriteRequest{
		Path:      "/test/file.txt",
		Content:   "test content",
		Overwrite: true,
		Backup:    false,
	}

	if result.Path != expected.Path {
		t.Errorf("Path = %v, want %v", result.Path, expected.Path)
	}
	if result.Content != expected.Content {
		t.Errorf("Content = %v, want %v", result.Content, expected.Content)
	}
	if result.Overwrite != expected.Overwrite {
		t.Errorf("Overwrite = %v, want %v", result.Overwrite, expected.Overwrite)
	}
	if result.Backup != expected.Backup {
		t.Errorf("Backup = %v, want %v", result.Backup, expected.Backup)
	}
}

// Helper function to check if a string contains a substring (simplified)
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findInStringParam(s, substr)
}

func findInStringParam(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
