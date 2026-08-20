package tools

import (
	"context"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractFilenameFromURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		contentType string
		want        string
	}{
		{
			name:        "URL with extension keeps it",
			url:         "https://example.com/photo.png",
			contentType: "",
			want:        "photo.png",
		},
		{
			name:        "extensionless URL with image/png gets .png",
			url:         "https://github.com/user-attachments/assets/6a8fffe9-d795-499f-8ef5-e470a97a14af",
			contentType: "image/png",
			want:        "6a8fffe9-d795-499f-8ef5-e470a97a14af.png",
		},
		{
			name:        "extensionless URL with image/jpeg gets extension from mime.ExtensionsByType",
			url:         "https://example.com/photo",
			contentType: "image/jpeg",
			want:        "photo.jfif",
		},
		{
			name:        "extensionless URL with unknown type gets .bin from mime.ExtensionsByType",
			url:         "https://example.com/file",
			contentType: "application/octet-stream",
			want:        "file.bin",
		},
		{
			name:        "extensionless URL with empty content type gets .dat",
			url:         "https://example.com/file",
			contentType: "",
			want:        "file.dat",
		},
		{
			name:        "URL with query string stripped",
			url:         "https://example.com/file.png?w=800",
			contentType: "",
			want:        "file.png",
		},
		{
			name:        "URL with fragment stripped",
			url:         "https://example.com/file.png#section",
			contentType: "",
			want:        "file.png",
		},
		{
			name:        "content type with charset parameter",
			url:         "https://example.com/photo",
			contentType: "image/png; charset=utf-8",
			want:        "photo.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFilenameFromURL(tt.url, tt.contentType)
			if got != tt.want {
				t.Errorf("extractFilenameFromURL(%q, %q) = %q, want %q", tt.url, tt.contentType, got, tt.want)
			}
		})
	}
}

// TestFetchTool_Execute_BinarySavedWithCorrectExtension proves that binary
// content fetched from an extensionless URL gets a filename derived from the
// response Content-Type (e.g. image/png -> .png) instead of .dat.
func TestFetchTool_Execute_BinarySavedWithCorrectExtension(t *testing.T) {
	pngBytes := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01, 0x02, 0xFF, 0xFE}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	tool := newHTTPTestFetchTool(t)
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL + "/image"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	fr, ok := result.Data.(*agentdomain.FetchResult)
	if !ok {
		t.Fatalf("expected *agentdomain.FetchResult, got %T", result.Data)
	}
	if !strings.HasSuffix(fr.SavedPath, ".png") {
		t.Errorf("expected saved file to end with .png, got: %s", fr.SavedPath)
	}
}
