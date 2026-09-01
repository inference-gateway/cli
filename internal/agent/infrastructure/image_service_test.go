package infrastructure

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
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	models "github.com/inference-gateway/cli/internal/platform/models"
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
			service := NewImageService(localImageConfig(), nil)
			result := service.IsImageURL(tt.url)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// localImageConfig returns a default config that permits image downloads from
// loopback addresses, so tests can reach their httptest fixture servers.
func localImageConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Image.AllowLocal = true
	return cfg
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
			service := NewImageService(localImageConfig(), nil)
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

	service := NewImageService(localImageConfig(), nil)
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

	service := NewImageService(localImageConfig(), nil)
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
			service := NewImageService(localImageConfig(), nil)
			result := service.IsImageFile(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestImageService_IsImageModel verifies that image-model detection is driven
// by gateway-reported modalities: "image" without "text" means image-gen;
// anything else (vision, text-only, unknown) is not.
func TestImageService_IsImageModel(t *testing.T) {
	imageMods := sdk.ModelModalities{
		Input:  []sdk.Modality{sdk.ModalityText, sdk.ModalityImage},
		Output: []sdk.Modality{sdk.ModalityImage},
	}
	textMods := sdk.ModelModalities{
		Input:  []sdk.Modality{sdk.ModalityText},
		Output: []sdk.Modality{sdk.ModalityText},
	}
	visionMods := sdk.ModelModalities{
		Input:  []sdk.Modality{sdk.ModalityText, sdk.ModalityImage},
		Output: []sdk.Modality{sdk.ModalityText},
	}
	models.SetGatewayModalities(map[string]sdk.ModelModalities{
		"openai/gpt-image-2":        imageMods,
		"openai/dall-e-3":           imageMods,
		"deepinfra/FLUX-1-schnell":  imageMods,
		"google/nano-banana":        imageMods,
		"openai/gpt-4o":             visionMods,
		"anthropic/claude-sonnet-5": textMods,
	})
	defer models.SetGatewayModalities(nil)

	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{"gpt-image", "openai/gpt-image-2", true},
		{"dall-e", "openai/dall-e-3", true},
		{"flux", "deepinfra/FLUX-1-schnell", true},
		{"nano-banana", "google/nano-banana", true},
		{"vision model", "openai/gpt-4o", false},
		{"text model", "anthropic/claude-sonnet-5", false},
		{"unknown model", "some/unknown-model", false},
		{"empty", "", false},
	}

	service := NewImageService(localImageConfig(), nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, service.IsImageModel(tt.model))
		})
	}
}

// fakeImageClient records the request and returns a canned response.
type fakeImageClient struct {
	sdk.Client
	gotProvider     sdk.Provider
	gotRequest      sdk.CreateImageRequest
	gotEditProvider sdk.Provider
	gotEditRequest  sdk.CreateImageEditMultipartBody
	response        *sdk.ImagesResponse
	err             error
}

func (f *fakeImageClient) CreateImage(_ context.Context, provider sdk.Provider, request sdk.CreateImageRequest) (*sdk.ImagesResponse, error) {
	f.gotProvider = provider
	f.gotRequest = request
	return f.response, f.err
}

func (f *fakeImageClient) CreateImageEdit(_ context.Context, provider sdk.Provider, request sdk.CreateImageEditMultipartBody) (*sdk.ImagesResponse, error) {
	f.gotEditProvider = provider
	f.gotEditRequest = request
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
	t.Setenv("HOME", t.TempDir())
	pngBytes := onePixelPNG(t)
	b64 := base64.StdEncoding.EncodeToString(pngBytes)

	t.Run("base64 payload is decoded and saved", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &b64}}}}
		service := NewImageService(localImageConfig(), client)

		t.Chdir(t.TempDir())
		path, err := service.GenerateImage(context.Background(), "openai/gpt-image-2", "a cat",
			"low", "1024x1024")

		assert.NoError(t, err)
		saved, readErr := os.ReadFile(path)
		assert.NoError(t, readErr)
		assert.Equal(t, pngBytes, saved)

		assert.Equal(t, sdk.Provider("openai"), client.gotProvider)
		assert.Equal(t, "gpt-image-2", *client.gotRequest.Model)
		assert.Equal(t, "a cat", client.gotRequest.Prompt)
		assert.Equal(t, sdk.CreateImageRequestQualityLow, *client.gotRequest.Quality)
		assert.Equal(t, sdk.ImageSize1024X1024, *client.gotRequest.Size)
	})

	t.Run("blank quality and size are omitted", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &b64}}}}
		service := NewImageService(localImageConfig(), client)

		t.Chdir(t.TempDir())
		_, err := service.GenerateImage(context.Background(), "openai/gpt-image-2", "a cat", "", "")

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
		service := NewImageService(localImageConfig(), client)

		t.Chdir(t.TempDir())
		path, err := service.GenerateImage(context.Background(), "openai/gpt-image-2", "a cat", "", "")

		assert.NoError(t, err)
		saved, readErr := os.ReadFile(path)
		assert.NoError(t, readErr)
		assert.Equal(t, pngBytes, saved)
	})

	t.Run("api error is returned", func(t *testing.T) {
		client := &fakeImageClient{err: fmt.Errorf("API error: Requested route is not found (status code: 404)")}
		service := NewImageService(localImageConfig(), client)

		_, err := service.GenerateImage(context.Background(), "openai/gpt-image-2", "a cat", "", "")
		assert.ErrorContains(t, err, "404")
	})

	t.Run("empty data is an error", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{}}
		service := NewImageService(localImageConfig(), client)

		_, err := service.GenerateImage(context.Background(), "openai/gpt-image-2", "a cat", "", "")
		assert.ErrorContains(t, err, "no images")
	})

	t.Run("model without a provider prefix is rejected", func(t *testing.T) {
		service := NewImageService(localImageConfig(), &fakeImageClient{})

		_, err := service.GenerateImage(context.Background(), "gpt-image-1", "a cat", "", "")
		assert.ErrorContains(t, err, "expected 'provider/model'")
	})
}

func TestImageService_EditImage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pngBytes := onePixelPNG(t)
	b64 := base64.StdEncoding.EncodeToString(pngBytes)

	// writeInput writes a valid PNG so ReadImageFromFile can decode it.
	writeInput := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "input.png")
		if err := os.WriteFile(path, pngBytes, 0o644); err != nil {
			t.Fatalf("writing input png: %v", err)
		}
		return path
	}

	t.Run("input image is uploaded and result is saved", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &b64}}}}
		service := NewImageService(localImageConfig(), client)
		input := writeInput(t)

		t.Chdir(t.TempDir())
		path, err := service.EditImage(context.Background(), "openai/gpt-image-2", "make it blue", input, "auto", "1024x1024", "")

		assert.NoError(t, err)
		saved, readErr := os.ReadFile(path)
		assert.NoError(t, readErr)
		assert.Equal(t, pngBytes, saved)

		assert.Equal(t, sdk.Provider("openai"), client.gotEditProvider)
		assert.Equal(t, "gpt-image-2", *client.gotEditRequest.Model)
		assert.Equal(t, "make it blue", client.gotEditRequest.Prompt)
		assert.Equal(t, sdk.CreateImageEditMultipartBodyQualityAuto, *client.gotEditRequest.Quality)
		assert.Equal(t, sdk.ImageSize1024X1024, *client.gotEditRequest.Size)

		uploaded, fileErr := client.gotEditRequest.Image.Bytes()
		assert.NoError(t, fileErr)
		assert.Equal(t, pngBytes, uploaded)
	})

	t.Run("blank quality and size are omitted", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &b64}}}}
		service := NewImageService(localImageConfig(), client)
		input := writeInput(t)

		t.Chdir(t.TempDir())
		_, err := service.EditImage(context.Background(), "openai/gpt-image-2", "make it blue", input, "", "", "")

		assert.NoError(t, err)
		assert.Nil(t, client.gotEditRequest.Quality)
		assert.Nil(t, client.gotEditRequest.Size)
		assert.Nil(t, client.gotEditRequest.Mask)
	})

	t.Run("mask is attached to the request", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &b64}}}}
		service := NewImageService(localImageConfig(), client)
		input := writeInput(t)
		mask := writeInput(t)

		t.Chdir(t.TempDir())
		_, err := service.EditImage(context.Background(), "openai/gpt-image-2", "make it blue", input, "", "", mask)

		assert.NoError(t, err)
		assert.NotNil(t, client.gotEditRequest.Mask)
		uploaded, fileErr := client.gotEditRequest.Mask.Bytes()
		assert.NoError(t, fileErr)
		assert.Equal(t, pngBytes, uploaded)
	})

	t.Run("missing mask file is an error", func(t *testing.T) {
		service := NewImageService(localImageConfig(), &fakeImageClient{})
		input := writeInput(t)

		_, err := service.EditImage(context.Background(), "openai/gpt-image-2", "make it blue", input, "", "", "/nonexistent/mask.png")
		assert.ErrorContains(t, err, "failed to read mask image")
	})

	t.Run("api error is returned", func(t *testing.T) {
		client := &fakeImageClient{err: fmt.Errorf("API error: Requested route is not found (status code: 404)")}
		service := NewImageService(localImageConfig(), client)
		input := writeInput(t)

		_, err := service.EditImage(context.Background(), "openai/gpt-image-2", "make it blue", input, "", "", "")
		assert.ErrorContains(t, err, "404")
	})

	t.Run("empty data is an error", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{}}
		service := NewImageService(localImageConfig(), client)
		input := writeInput(t)

		_, err := service.EditImage(context.Background(), "openai/gpt-image-2", "make it blue", input, "", "", "")
		assert.ErrorContains(t, err, "no images")
	})

	t.Run("missing input file is an error", func(t *testing.T) {
		service := NewImageService(localImageConfig(), &fakeImageClient{})

		_, err := service.EditImage(context.Background(), "openai/gpt-image-2", "make it blue", "/nonexistent/input.png", "", "", "")
		assert.ErrorContains(t, err, "failed to read image file")
	})

	t.Run("model without a provider prefix is rejected", func(t *testing.T) {
		service := NewImageService(localImageConfig(), &fakeImageClient{})
		input := writeInput(t)

		_, err := service.EditImage(context.Background(), "gpt-image-1", "make it blue", input, "", "", "")
		assert.ErrorContains(t, err, "expected 'provider/model'")
	})
}

func TestImageService_CreateImageVariation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pngBytes := onePixelPNG(t)
	b64 := base64.StdEncoding.EncodeToString(pngBytes)

	writeInput := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "input.png")
		if err := os.WriteFile(path, pngBytes, 0o644); err != nil {
			t.Fatalf("writing input png: %v", err)
		}
		return path
	}

	t.Run("input image is uploaded via edits endpoint and result is saved", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &b64}}}}
		service := NewImageService(localImageConfig(), client)
		input := writeInput(t)

		t.Chdir(t.TempDir())
		path, err := service.CreateImageVariation(context.Background(), "openai/gpt-image-2", input, "1024x1024")

		assert.NoError(t, err)
		saved, readErr := os.ReadFile(path)
		assert.NoError(t, readErr)
		assert.Equal(t, pngBytes, saved)

		assert.Equal(t, sdk.Provider("openai"), client.gotEditProvider)
		assert.Equal(t, "gpt-image-2", *client.gotEditRequest.Model)
		assert.Equal(t, variationPrompt, client.gotEditRequest.Prompt)
		assert.Equal(t, sdk.ImageSize1024X1024, *client.gotEditRequest.Size)

		uploaded, fileErr := client.gotEditRequest.Image.Bytes()
		assert.NoError(t, fileErr)
		assert.Equal(t, pngBytes, uploaded)
	})

	t.Run("blank size is omitted", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{Data: []sdk.Image{{B64Json: &b64}}}}
		service := NewImageService(localImageConfig(), client)
		input := writeInput(t)

		t.Chdir(t.TempDir())
		_, err := service.CreateImageVariation(context.Background(), "openai/gpt-image-2", input, "")

		assert.NoError(t, err)
		assert.Nil(t, client.gotEditRequest.Size)
	})

	t.Run("api error is returned", func(t *testing.T) {
		client := &fakeImageClient{err: fmt.Errorf("API error: Requested route is not found (status code: 404)")}
		service := NewImageService(localImageConfig(), client)
		input := writeInput(t)

		_, err := service.CreateImageVariation(context.Background(), "openai/gpt-image-2", input, "")
		assert.ErrorContains(t, err, "404")
	})

	t.Run("empty data is an error", func(t *testing.T) {
		client := &fakeImageClient{response: &sdk.ImagesResponse{}}
		service := NewImageService(localImageConfig(), client)
		input := writeInput(t)

		_, err := service.CreateImageVariation(context.Background(), "openai/gpt-image-2", input, "")
		assert.ErrorContains(t, err, "no images")
	})

	t.Run("missing input file is an error", func(t *testing.T) {
		service := NewImageService(localImageConfig(), &fakeImageClient{})

		_, err := service.CreateImageVariation(context.Background(), "openai/gpt-image-2", "/nonexistent/input.png", "")
		assert.ErrorContains(t, err, "failed to read image file")
	})

	t.Run("model without a provider prefix is rejected", func(t *testing.T) {
		service := NewImageService(localImageConfig(), &fakeImageClient{})
		input := writeInput(t)

		_, err := service.CreateImageVariation(context.Background(), "gpt-image-1", input, "")
		assert.ErrorContains(t, err, "expected 'provider/model'")
	})
}

func TestImageService_ReadImageFromURL_BlocksNonPublicAndBadSchemes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	service := NewImageService(config.DefaultConfig(), nil)

	_, err := service.ReadImageFromURL(server.URL + "/img.png")
	assert.Error(t, err, "loopback URLs must be refused")

	_, err = service.ReadImageFromURL("file:///etc/passwd")
	assert.ErrorContains(t, err, "unsupported image URL scheme")
}
