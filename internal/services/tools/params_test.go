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
				FilePath:  "/test/file.txt",
				Content:   "test content",
				Overwrite: true,
				Append:    false,
				Backup:    false,
				IsChunked: false,
			},
		},
		{
			name: "valid params with all options",
			params: map[string]interface{}{
				"file_path": "/test/file.txt",
				"content":   "test content",
				"append":    true,
				"overwrite": false,
				"backup":    true,
			},
			expected: &WriteParams{
				FilePath:  "/test/file.txt",
				Content:   "test content",
				Append:    true,
				Overwrite: false,
				Backup:    true,
				IsChunked: false,
			},
		},
		{
			name: "valid chunked params",
			params: map[string]interface{}{
				"file_path":    "/test/file.txt",
				"content":      "chunk content",
				"session_id":   "session123",
				"chunk_index":  0,
				"total_chunks": 3,
			},
			expected: &WriteParams{
				FilePath:    "/test/file.txt",
				Content:     "chunk content",
				Overwrite:   true,
				SessionID:   "session123",
				ChunkIndex:  0,
				TotalChunks: 3,
				IsChunked:   true,
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
		{
			name: "chunked with missing session_id",
			params: map[string]interface{}{
				"file_path":   "/test/file.txt",
				"content":     "chunk content",
				"chunk_index": 0,
			},
			wantError: true,
			errorMsg:  "session_id is required for chunked operations",
		},
		{
			name: "chunked with negative chunk_index",
			params: map[string]interface{}{
				"file_path":   "/test/file.txt",
				"content":     "chunk content",
				"session_id":  "session123",
				"chunk_index": -1,
			},
			wantError: true,
			errorMsg:  "chunk_index is required for chunked operations",
		},
		{
			name: "append with chunked (invalid combination)",
			params: map[string]interface{}{
				"file_path":   "/test/file.txt",
				"content":     "chunk content",
				"append":      true,
				"session_id":  "session123",
				"chunk_index": 0,
			},
			wantError: true,
			errorMsg:  "append mode is not supported with chunked operations",
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
	if result.Append != expected.Append {
		t.Errorf("Append = %v, want %v", result.Append, expected.Append)
	}
	if result.Overwrite != expected.Overwrite {
		t.Errorf("Overwrite = %v, want %v", result.Overwrite, expected.Overwrite)
	}
	if result.Backup != expected.Backup {
		t.Errorf("Backup = %v, want %v", result.Backup, expected.Backup)
	}
	if result.IsChunked != expected.IsChunked {
		t.Errorf("IsChunked = %v, want %v", result.IsChunked, expected.IsChunked)
	}
	if result.IsChunked {
		validateChunkedParams(t, result, expected)
	}
}

func validateChunkedParams(t *testing.T, result, expected *WriteParams) {
	if result.SessionID != expected.SessionID {
		t.Errorf("SessionID = %v, want %v", result.SessionID, expected.SessionID)
	}
	if result.ChunkIndex != expected.ChunkIndex {
		t.Errorf("ChunkIndex = %v, want %v", result.ChunkIndex, expected.ChunkIndex)
	}
	if result.TotalChunks != expected.TotalChunks {
		t.Errorf("TotalChunks = %v, want %v", result.TotalChunks, expected.TotalChunks)
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
		FilePath:  "/test/file.txt",
		Content:   "test content",
		Overwrite: true,
		Backup:    true,
	}

	result := extractor.ToWriteRequest(params)

	expected := filewriter.WriteRequest{
		Path:      "/test/file.txt",
		Content:   "test content",
		Overwrite: true,
		Backup:    true,
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

func TestParameterExtractor_ToChunkWriteRequest(t *testing.T) {
	extractor := NewParameterExtractor()

	params := &WriteParams{
		Content:     "chunk content",
		SessionID:   "session123",
		ChunkIndex:  2,
		TotalChunks: 3,
	}

	result := extractor.ToChunkWriteRequest(params)

	if result.SessionID != "session123" {
		t.Errorf("SessionID = %v, want %v", result.SessionID, "session123")
	}
	if result.ChunkIndex != 2 {
		t.Errorf("ChunkIndex = %v, want %v", result.ChunkIndex, 2)
	}
	if string(result.Data) != "chunk content" {
		t.Errorf("Data = %v, want %v", string(result.Data), "chunk content")
	}
	if !result.IsLast {
		t.Errorf("IsLast = %v, want %v", result.IsLast, true)
	}
}

func TestParameterExtractor_BooleanConversions(t *testing.T) {
	extractor := NewParameterExtractor()

	tests := []struct {
		name     string
		params   map[string]interface{}
		key      string
		expected bool
	}{
		{"true string", map[string]interface{}{"key": "true"}, "key", true},
		{"false string", map[string]interface{}{"key": "false"}, "key", false},
		{"1 string", map[string]interface{}{"key": "1"}, "key", true},
		{"0 string", map[string]interface{}{"key": "0"}, "key", false},
		{"yes string", map[string]interface{}{"key": "yes"}, "key", true},
		{"no string", map[string]interface{}{"key": "no"}, "key", false},
		{"true bool", map[string]interface{}{"key": true}, "key", true},
		{"false bool", map[string]interface{}{"key": false}, "key", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractor.extractBool(tt.params, tt.key, false)
			if result != tt.expected {
				t.Errorf("extractBool() = %v, want %v", result, tt.expected)
			}
		})
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
