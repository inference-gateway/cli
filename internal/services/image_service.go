package services

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

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
	// Read image file
	imageData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read image file: %w", err)
	}

	// Detect image format
	_, format, err := image.DecodeConfig(bytes.NewReader(imageData))
	if err != nil {
		return nil, fmt.Errorf("failed to detect image format: %w", err)
	}

	// Convert to base64
	base64Data := base64.StdEncoding.EncodeToString(imageData)

	// Determine MIME type
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
	if len(filePath) < 4 {
		return false
	}
	// Check file extension
	ext := filePath[len(filePath)-4:]
	return ext == ".png" || ext == ".jpg" || ext == "jpeg" || ext == ".gif" || ext == "webp"
}
