package services

import (
	"bytes"
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

	domain "github.com/inference-gateway/cli/internal/domain"
)

// ImageService handles image operations including file-based image loading and base64 encoding
// Note: Direct clipboard support requires platform-specific dependencies and is not yet implemented
type ImageService struct{}

// NewImageService creates a new image service
func NewImageService() *ImageService {
	return &ImageService{}
}

// ReadImageFromFile reads an image from a file path and returns it as a base64 attachment
func (s *ImageService) ReadImageFromFile(filePath string) (*domain.ImageAttachment, error) {
	filePath = s.normalizeFilePath(filePath)

	imageData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	return s.ReadImageFromBinary(imageData, filePath)
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
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Fetch the image
	resp, err := client.Get(imageURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch image from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch image: HTTP %d", resp.StatusCode)
	}

	// Read the image data
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	// Extract filename from URL
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
	// Check if it's a valid URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return false
	}

	// Must have http or https scheme
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return false
	}

	// Check if the URL path has an image extension
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
