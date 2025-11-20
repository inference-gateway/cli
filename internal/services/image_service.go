package services

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	if strings.HasPrefix(filePath, "file://") {
		parsedURL, err := url.Parse(filePath)
		if err != nil {
			return nil, fmt.Errorf("invalid file URL: %w", err)
		}
		filePath = parsedURL.Path
	}

	imageData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	_, format, err := image.DecodeConfig(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to detect image format: %w", err)
	}

	base64Data := base64.StdEncoding.EncodeToString(imageData)

	mimeType := fmt.Sprintf("image/%s", format)

	return &domain.ImageAttachment{
		Data:     base64Data,
		MimeType: mimeType,
		Filename: filePath,
	}, nil
}

// CreateDataURL creates a data URL from an image attachment
func (s *ImageService) CreateDataURL(attachment *domain.ImageAttachment) string {
	return fmt.Sprintf("data:%s;base64,%s", attachment.MimeType, attachment.Data)
}

// IsImageFile checks if a file is a supported image format
func (s *ImageService) IsImageFile(filePath string) bool {
	// Handle file:// URLs
	if strings.HasPrefix(filePath, "file://") {
		parsedURL, err := url.Parse(filePath)
		if err != nil {
			return false
		}
		filePath = parsedURL.Path
	}

	// Get file extension
	ext := strings.ToLower(filepath.Ext(filePath))
	supportedExts := []string{".png", ".jpg", ".jpeg", ".gif", ".webp"}

	for _, supportedExt := range supportedExts {
		if ext == supportedExt {
			return true
		}
	}

	return false
}
