package services

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestImageService_IsImageURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{
			name:     "Valid HTTP image URL",
			url:      "http://example.com/image.png",
			expected: true,
		},
		{
			name:     "Valid HTTPS image URL",
			url:      "https://example.com/photo.jpg",
			expected: true,
		},
		{
			name:     "Valid image URL with path",
			url:      "https://example.com/assets/images/logo.png",
			expected: true,
		},
		{
			name:     "Invalid - no scheme",
			url:      "example.com/image.png",
			expected: false,
		},
		{
			name:     "Invalid - file scheme",
			url:      "file:///path/to/image.png",
			expected: false,
		},
		{
			name:     "Invalid - no image extension",
			url:      "https://example.com/page.html",
			expected: false,
		},
		{
			name:     "Invalid - not a URL",
			url:      "not-a-url",
			expected: false,
		},
		{
			name:     "Valid - JPEG extension",
			url:      "https://example.com/photo.jpeg",
			expected: true,
		},
		{
			name:     "Valid - GIF extension",
			url:      "https://example.com/animation.gif",
			expected: true,
		},
		{
			name:     "Valid - WebP extension",
			url:      "https://example.com/modern.webp",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewImageService()
			result := service.IsImageURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImageService_ReadImageFromURL(t *testing.T) {
	// Create a test server that serves a 1x1 PNG
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pngData)
	}))
	defer server.Close()

	tests := []struct {
		name        string
		url         string
		expectError bool
	}{
		{
			name:        "Valid image URL",
			url:         server.URL + "/test.png",
			expectError: false,
		},
		{
			name:        "Invalid URL",
			url:         "http://nonexistent-domain-12345.com/image.png",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewImageService()
			attachment, err := service.ReadImageFromURL(tt.url)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, attachment)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, attachment)
				assert.NotEmpty(t, attachment.Data)
				assert.Equal(t, "image/png", attachment.MimeType)
			}
		})
	}
}

func TestImageService_ReadImageFromURL_404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	service := NewImageService()
	attachment, err := service.ReadImageFromURL(server.URL + "/notfound.png")

	assert.Error(t, err)
	assert.Nil(t, attachment)
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestImageService_IsImageFile(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "PNG file",
			path:     "image.png",
			expected: true,
		},
		{
			name:     "JPG file",
			path:     "photo.jpg",
			expected: true,
		},
		{
			name:     "JPEG file",
			path:     "photo.jpeg",
			expected: true,
		},
		{
			name:     "GIF file",
			path:     "animation.gif",
			expected: true,
		},
		{
			name:     "WebP file",
			path:     "modern.webp",
			expected: true,
		},
		{
			name:     "Text file",
			path:     "document.txt",
			expected: false,
		},
		{
			name:     "Go file",
			path:     "main.go",
			expected: false,
		},
		{
			name:     "No extension",
			path:     "file",
			expected: false,
		},
		{
			name:     "File URL with PNG",
			path:     "file:///path/to/image.png",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewImageService()
			result := service.IsImageFile(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
