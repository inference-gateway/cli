package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	config "github.com/inference-gateway/cli/config"
	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
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
			service := NewImageService(config.DefaultConfig(), nil)
			result := service.IsImageURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImageService_ReadImageFromURL(t *testing.T) {
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
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
			service := NewImageService(config.DefaultConfig(), nil)
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

	service := NewImageService(config.DefaultConfig(), nil)
	attachment, err := service.ReadImageFromURL(server.URL + "/notfound.png")

	assert.Error(t, err)
	assert.Nil(t, attachment)
	assert.Contains(t, err.Error(), "HTTP 404")
}

func TestImageService_ReadImageFromURL_OversizedImage(t *testing.T) {
	largeData := make([]byte, 6*1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(largeData)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(largeData)
	}))
	defer server.Close()

	service := NewImageService(config.DefaultConfig(), nil)
	attachment, err := service.ReadImageFromURL(server.URL + "/large.png")

	assert.Error(t, err)
	assert.Nil(t, attachment)
	assert.Contains(t, err.Error(), "exceeds maximum")
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
			service := NewImageService(config.DefaultConfig(), nil)
			result := service.IsImageFile(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestImageService_IsImageModel(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{"gpt-image", "openai/gpt-image-1", true},
		{"dall-e", "openai/dall-e-3", true},
		{"flux", "deepinfra/FLUX-1-schnell", true},
		{"nano-banana", "google/nano-banana", true},
		{"text model", "openai/gpt-4o", false},
		{"text model with image in provider path", "anthropic/claude-sonnet-5", false},
		{"empty", "", false},
	}

	service := NewImageService(config.DefaultConfig(), nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, service.IsImageModel(tt.model))
		})
	}
}

// fakeImageClient records the request and returns a canned response.
type fakeImageClient struct {
	sdk.Client
	gotProvider sdk.Provider
	gotRequest  sdk.CreateImageRequest
	response    *sdk.ImagesResponse
	err         error
}

func (f *fakeImageClient) CreateImage(_ context.Context, provider sdk.Provider, request sdk.CreateImageRequest) (*sdk.ImagesResponse, error) {
	f.gotProvider = provider
	f.gotRequest = request
	return f.response, f.err
}

// onePixelPNG is the smallest valid PNG, so ReadImageFromBinary can decode it.
func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding png: %v", err)
	}
	return buf.Bytes()
}

func TestImageService_GenerateImage(t *testing.T) {
	pngBytes := onePixelPNG(t)
	b64 := base64.StdEncoding.EncodeToString(pngBytes)

	t.Run("base64 payload is decoded and saved", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &b64}}}}
		service := NewImageService(config.DefaultConfig(), client)

		t.Chdir(t.TempDir())
		path, err := service.GenerateImage(context.Background(), "openai/gpt-image-1", "a cat",
			"low", "1024x1024")

		assert.NoError(t, err)
		saved, readErr := os.ReadFile(path)
		assert.NoError(t, readErr)
		assert.Equal(t, pngBytes, saved)

		assert.Equal(t, sdk.Provider("openai"), client.gotProvider)
		assert.Equal(t, "gpt-image-1", *client.gotRequest.Model)
		assert.Equal(t, "a cat", client.gotRequest.Prompt)
		assert.Equal(t, sdk.CreateImageRequestQualityLow, *client.gotRequest.Quality)
		assert.Equal(t, sdk.ImageSize1024X1024, *client.gotRequest.Size)
	})

	t.Run("blank quality and size are omitted", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &b64}}}}
		service := NewImageService(config.DefaultConfig(), client)

		t.Chdir(t.TempDir())
		_, err := service.GenerateImage(context.Background(), "openai/gpt-image-1", "a cat", "", "")

		assert.NoError(t, err)
		assert.Nil(t, client.gotRequest.Quality)
		assert.Nil(t, client.gotRequest.Size)
	})

	t.Run("url payload is downloaded", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(pngBytes)
		}))
		defer server.Close()

		url := server.URL + "/generated.png"
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{URL: &url}}}}
		service := NewImageService(config.DefaultConfig(), client)

		t.Chdir(t.TempDir())
		path, err := service.GenerateImage(context.Background(), "openai/gpt-image-1", "a cat", "", "")

		assert.NoError(t, err)
		saved, readErr := os.ReadFile(path)
		assert.NoError(t, readErr)
		assert.Equal(t, pngBytes, saved)
	})

	t.Run("api error is returned", func(t *testing.T) {
		client := &fakeImageClient{err: fmt.Errorf("API error: Requested route is not found (status code: 404)")}
		service := NewImageService(config.DefaultConfig(), client)

		_, err := service.GenerateImage(context.Background(), "openai/gpt-image-1", "a cat", "", "")
		assert.ErrorContains(t, err, "404")
	})

	t.Run("empty data is an error", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{}}
		service := NewImageService(config.DefaultConfig(), client)

		_, err := service.GenerateImage(context.Background(), "openai/gpt-image-1", "a cat", "", "")
		assert.ErrorContains(t, err, "no images")
	})

	t.Run("model without a provider prefix is rejected", func(t *testing.T) {
		service := NewImageService(config.DefaultConfig(), &fakeImageClient{})

		_, err := service.GenerateImage(context.Background(), "gpt-image-1", "a cat", "", "")
		assert.ErrorContains(t, err, "expected 'provider/model'")
	})
}
