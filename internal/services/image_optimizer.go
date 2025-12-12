package services

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"

	"github.com/inference-gateway/cli/config"
	"golang.org/x/image/draw"
)

// ImageOptimizer handles image optimization for clipboard images
type ImageOptimizer struct {
	config config.ClipboardImageOptimizeConfig
}

// NewImageOptimizer creates a new image optimizer with the given configuration
func NewImageOptimizer(cfg config.ClipboardImageOptimizeConfig) *ImageOptimizer {
	return &ImageOptimizer{
		config: cfg,
	}
}

// OptimizeResult contains the optimized image data and format information
type OptimizeResult struct {
	Data      []byte
	Extension string
}

// OptimizeImage optimizes an image file according to configuration
// Returns the optimized image data, extension, and any error encountered
func (o *ImageOptimizer) OptimizeImage(imagePath string) (*OptimizeResult, error) {
	if !o.config.Enabled {
		data, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, err
		}
		ext := "png"
		if len(imagePath) > 4 {
			ext = imagePath[len(imagePath)-3:]
		}
		return &OptimizeResult{Data: data, Extension: ext}, nil
	}

	file, err := os.Open(imagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open image: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	img, format, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	img = o.resizeIfNeeded(img)

	data, outputFormat, err := o.encodeImage(img, format)
	if err != nil {
		return nil, err
	}

	return &OptimizeResult{Data: data, Extension: outputFormat}, nil
}

// resizeIfNeeded resizes the image if it exceeds max dimensions
func (o *ImageOptimizer) resizeIfNeeded(img image.Image) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	needsResize := false
	newWidth := width
	newHeight := height

	if o.config.MaxWidth > 0 && width > o.config.MaxWidth {
		needsResize = true
		ratio := float64(o.config.MaxWidth) / float64(width)
		newWidth = o.config.MaxWidth
		newHeight = int(float64(height) * ratio)
	}

	if o.config.MaxHeight > 0 && newHeight > o.config.MaxHeight {
		needsResize = true
		ratio := float64(o.config.MaxHeight) / float64(newHeight)
		newHeight = o.config.MaxHeight
		newWidth = int(float64(newWidth) * ratio)
	}

	if !needsResize {
		return img
	}

	resized := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.CatmullRom.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)

	return resized
}

// encodeImage encodes the image to bytes based on configuration
// Returns image data, file extension, and error
func (o *ImageOptimizer) encodeImage(img image.Image, originalFormat string) ([]byte, string, error) {
	var buf bytes.Buffer

	if o.config.ConvertJPEG && originalFormat != "jpeg" {
		quality := o.config.Quality
		if quality <= 0 || quality > 100 {
			quality = 75
		}

		opts := &jpeg.Options{Quality: quality}
		if err := jpeg.Encode(&buf, img, opts); err != nil {
			return nil, "", fmt.Errorf("failed to encode JPEG: %w", err)
		}
		return buf.Bytes(), "jpg", nil
	}

	switch originalFormat {
	case "jpeg", "jpg":
		quality := o.config.Quality
		if quality <= 0 || quality > 100 {
			quality = 75
		}
		opts := &jpeg.Options{Quality: quality}
		if err := jpeg.Encode(&buf, img, opts); err != nil {
			return nil, "", fmt.Errorf("failed to encode JPEG: %w", err)
		}
		return buf.Bytes(), "jpg", nil

	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", fmt.Errorf("failed to encode PNG: %w", err)
		}
		return buf.Bytes(), "png", nil

	default:
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", fmt.Errorf("failed to encode as PNG: %w", err)
		}
		return buf.Bytes(), "png", nil
	}
}
