package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"
	openapi_types "github.com/oapi-codegen/runtime/types"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
)

// IsImageModel reports whether the model generates images rather than text.
// Uses gateway-reported modalities: a model is an image-generation model if
// its modalities include "image" but NOT "text". Falls back to false when
// the model is not in the registry.
func (s *ImageService) IsImageModel(model string) bool {
	return models.IsImageGenerationModel(model)
}

// ImageService handles image operations including file-based image loading and base64 encoding
// Note: Direct clipboard support requires platform-specific dependencies and is not yet implemented
type ImageService struct {
	config *config.Config
	client sdk.Client
}

// NewImageService creates a new image service
func NewImageService(cfg *config.Config, client sdk.Client) *ImageService {
	return &ImageService{
		config: cfg,
		client: client,
	}
}

// GenerateImage generates an image from prompt using model ("provider/name")
// and returns the path of the saved PNG. A blank quality or size is omitted
// from the request, leaving the provider's own default.
func (s *ImageService) GenerateImage(ctx context.Context, model, prompt, quality, size string) (string, error) {
	provider, modelName, ok := strings.Cut(model, "/")
	if !ok {
		return "", fmt.Errorf("invalid model %q (expected 'provider/model')", model)
	}

	request := sdk.CreateImageRequest{
		Model:  &modelName,
		Prompt: prompt,
	}
	if quality != "" {
		q := sdk.CreateImageRequestQuality(quality)
		request.Quality = &q
	}
	if size != "" {
		sz := sdk.ImageSize(size)
		request.Size = &sz
	}

	resp, err := s.client.CreateImage(ctx, sdk.Provider(provider), request)
	if err != nil {
		return "", err
	}
	if len(resp.Data) == 0 {
		return "", fmt.Errorf("image generation returned no images")
	}

	data, err := s.imageBytes(resp.Data[0])
	if err != nil {
		return "", err
	}

	return s.saveImage(ctx, data)
}

// EditImage edits the image at imagePath using prompt and model
// ("provider/name") and returns the path of the saved PNG. A blank quality or
// size is omitted from the request, leaving the provider's own default. A
// non-empty maskPath points to a PNG whose transparent (alpha=0) areas mark
// the editable region; all other pixels are preserved exactly.
func (s *ImageService) EditImage(ctx context.Context, model, prompt, imagePath, quality, size, maskPath string) (string, error) {
	provider, modelName, ok := strings.Cut(model, "/")
	if !ok {
		return "", fmt.Errorf("invalid model %q (expected 'provider/model')", model)
	}

	attachment, err := s.ReadImageFromFile(imagePath)
	if err != nil {
		return "", err
	}

	imageBytes, err := base64.StdEncoding.DecodeString(attachment.Data)
	if err != nil {
		return "", fmt.Errorf("failed to decode input image: %w", err)
	}

	var imageFile openapi_types.File
	imageFile.InitFromBytes(imageBytes, attachment.Filename)

	request := sdk.CreateImageEditMultipartBody{
		Model:  &modelName,
		Prompt: prompt,
		Image:  imageFile,
	}
	if quality != "" {
		q := sdk.CreateImageEditMultipartBodyQuality(quality)
		request.Quality = &q
	}
	if size != "" {
		sz := sdk.ImageSize(size)
		request.Size = &sz
	}
	if maskPath != "" {
		maskAttachment, err := s.ReadImageFromFile(maskPath)
		if err != nil {
			return "", fmt.Errorf("failed to read mask image: %w", err)
		}
		maskBytes, err := base64.StdEncoding.DecodeString(maskAttachment.Data)
		if err != nil {
			return "", fmt.Errorf("failed to decode mask image: %w", err)
		}
		var maskFile openapi_types.File
		maskFile.InitFromBytes(maskBytes, maskAttachment.Filename)
		request.Mask = &maskFile
	}

	resp, err := s.client.CreateImageEdit(ctx, sdk.Provider(provider), request)
	if err != nil {
		return "", err
	}
	if len(resp.Data) == 0 {
		return "", fmt.Errorf("image edit returned no images")
	}

	data, err := s.imageBytes(resp.Data[0])
	if err != nil {
		return "", err
	}

	return s.saveImage(ctx, data)
}

// variationPrompt drives edits-backed image variations: OpenAI's dedicated
// /v1/images/variations endpoint only ever supported the sunset dall-e-2
// model, so variations are produced through the edits endpoint instead.
const variationPrompt = "Create a variation of this image, preserving its subject, composition and overall style."

// CreateImageVariation creates a variation of the image at imagePath using
// model ("provider/name") and returns the path of the saved PNG. A blank size
// is omitted from the request, leaving the provider's own default.
func (s *ImageService) CreateImageVariation(ctx context.Context, model, imagePath, size string) (string, error) {
	return s.EditImage(ctx, model, variationPrompt, imagePath, "", size, "")
}

// saveImage writes image bytes to a timestamped PNG under the session's
// artifacts dir.
func (s *ImageService) saveImage(ctx context.Context, data []byte) (string, error) {
	dir := s.config.SessionArtifactsDir(domain.GetSessionID(ctx))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create %s: %w", dir, err)
	}
	path := filepath.Join(dir, fmt.Sprintf("image-%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("failed to save image: %w", err)
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path, nil
}

// imageBytes resolves a generated image payload: base64 data first, falling
// back to downloading the URL.
func (s *ImageService) imageBytes(img sdk.Image) ([]byte, error) {
	if img.B64Json != nil && *img.B64Json != "" {
		return base64.StdEncoding.DecodeString(*img.B64Json)
	}
	if img.URL != nil && *img.URL != "" {
		attachment, err := s.ReadImageFromURL(*img.URL)
		if err != nil {
			return nil, err
		}
		return base64.StdEncoding.DecodeString(attachment.Data)
	}
	return nil, fmt.Errorf("image response contained neither base64 data nor a URL")
}

// ReadImageFromFile reads an image from a file path and returns it as a base64 attachment
func (s *ImageService) ReadImageFromFile(filePath string) (*domain.ImageAttachment, error) {
	filePath = s.normalizeFilePath(filePath)

	imageData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	attachment, err := s.ReadImageFromBinary(imageData, filePath)
	if err != nil {
		return nil, err
	}
	attachment.SourcePath = filePath
	attachment.DisplayName = filepath.Base(filePath)
	return attachment, nil
}

// ReadImageFromBinary reads an image from binary data and returns it as a base64 attachment
func (s *ImageService) ReadImageFromBinary(imageData []byte, filename string) (*domain.ImageAttachment, error) {
	_, format, err := image.DecodeConfig(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to detect image format: %w", err)
	}

	base64Data := base64.StdEncoding.EncodeToString(imageData)
	mimeType := fmt.Sprintf("image/%s", format)

	return &domain.ImageAttachment{
		Data:     base64Data,
		MimeType: mimeType,
		Filename: filename,
	}, nil
}

// normalizeFilePath converts file:// URLs to regular paths
func (s *ImageService) normalizeFilePath(filePath string) string {
	if strings.HasPrefix(filePath, "file://") {
		parsedURL, err := url.Parse(filePath)
		if err != nil {
			return filePath
		}
		return parsedURL.Path
	}
	return filePath
}

// CreateDataURL creates a data URL from an image attachment
func (s *ImageService) CreateDataURL(attachment *domain.ImageAttachment) string {
	return fmt.Sprintf("data:%s;base64,%s", attachment.MimeType, attachment.Data)
}

// IsImageFile checks if a file is a supported image format
func (s *ImageService) IsImageFile(filePath string) bool {
	filePath = s.normalizeFilePath(filePath)
	ext := strings.ToLower(filepath.Ext(filePath))

	supportedExts := map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
		".webp": true,
	}

	return supportedExts[ext]
}

// ReadImageFromURL fetches an image from a URL and returns it as a base64 attachment
func (s *ImageService) ReadImageFromURL(imageURL string) (*domain.ImageAttachment, error) {
	timeout := time.Duration(s.config.Image.Timeout) * time.Second
	client := &http.Client{
		Timeout: timeout,
	}

	resp, err := client.Get(imageURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image from URL: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch image: HTTP %d", resp.StatusCode)
	}

	maxSize := s.config.Image.MaxSize
	if resp.ContentLength > 0 && resp.ContentLength > maxSize {
		return nil, fmt.Errorf("image size (%d bytes) exceeds maximum allowed size (%d bytes)", resp.ContentLength, maxSize)
	}

	imageData, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	if int64(len(imageData)) >= maxSize {
		return nil, fmt.Errorf("image exceeds maximum size of %d bytes", maxSize)
	}

	parsedURL, err := url.Parse(imageURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}
	filename := filepath.Base(parsedURL.Path)
	if filename == "" || filename == "." || filename == "/" {
		filename = "image"
	}

	return s.ReadImageFromBinary(imageData, filename)
}

// IsImageURL checks if a string is a valid image URL
func (s *ImageService) IsImageURL(urlStr string) bool {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return false
	}

	ext := strings.ToLower(filepath.Ext(parsedURL.Path))
	supportedExts := map[string]bool{
		".png":  true,
		".jpg":  true,
		".jpeg": true,
		".gif":  true,
		".webp": true,
	}

	return supportedExts[ext]
}
