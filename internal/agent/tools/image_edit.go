package tools

import (
	"context"
	"fmt"
	"slices"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// ImageEditTool edits an existing image with the configured image model and
// saves the result to a local PNG. The chat model calls it with a local image
// path and a plain prompt; the request goes straight to /v1/images/edits,
// independent of the model selected for the session.
type ImageEditTool struct {
	config       *config.Config
	imageService domain.ImageService
}

var (
	imageEditQualities = []string{
		string(sdk.CreateImageEditMultipartBodyQualityAuto),
		string(sdk.CreateImageEditMultipartBodyQualityLow),
		string(sdk.CreateImageEditMultipartBodyQualityMedium),
		string(sdk.CreateImageEditMultipartBodyQualityHigh),
		string(sdk.CreateImageEditMultipartBodyQualityStandard),
	}
)

// NewImageEditTool creates a new ImageEdit tool
func NewImageEditTool(cfg *config.Config, imageService domain.ImageService) *ImageEditTool {
	return &ImageEditTool{
		config:       cfg,
		imageService: imageService,
	}
}

// Definition returns the tool definition for ImageEdit
func (t *ImageEditTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.ImageEdit.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ImageEdit",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"image": map[string]any{
						"type":        "string",
						"description": "Local file path of the image to edit",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "A text description of the desired edit",
					},
					"quality": map[string]any{
						"type":        "string",
						"enum":        imageEditQualities,
						"description": "Image quality. Always use 'auto' unless the user explicitly asks for a different tier",
						"default":     string(sdk.CreateImageEditMultipartBodyQualityAuto),
					},
					"size": map[string]any{
						"type":        "string",
						"enum":        imageSizes,
						"description": "Image size. Always use '1024x1024' unless the user explicitly asks for a larger or differently shaped image",
						"default":     string(sdk.ImageSize1024X1024),
					},
				},
				"required":             []string{"image", "prompt"},
				"additionalProperties": false,
			},
		},
	}
}

// Validate validates ImageEdit arguments
func (t *ImageEditTool) Validate(args map[string]any) error {
	image, ok := args["image"].(string)
	if !ok || image == "" {
		return fmt.Errorf("image is required and must be a non-empty file path")
	}
	if !t.imageService.IsImageFile(image) {
		return fmt.Errorf("image must point to a supported image file (png, jpg, jpeg, gif, webp)")
	}

	prompt, ok := args["prompt"].(string)
	if !ok || prompt == "" {
		return fmt.Errorf("prompt is required and must be a non-empty string")
	}

	if raw, ok := args["quality"]; ok {
		quality, ok := raw.(string)
		if !ok || !slices.Contains(imageEditQualities, quality) {
			return fmt.Errorf("quality must be one of: auto, low, medium, high, standard")
		}
	}

	if raw, ok := args["size"]; ok {
		size, ok := raw.(string)
		if !ok || !slices.Contains(imageSizes, size) {
			return fmt.Errorf("size must be one of: 1024x1024, 1536x1024, 1024x1536")
		}
	}

	return nil
}

// Execute executes the ImageEdit tool
func (t *ImageEditTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	image, _ := args["image"].(string)
	prompt, _ := args["prompt"].(string)
	quality, _ := args["quality"].(string)
	size, _ := args["size"].(string)

	if quality == "" {
		quality = string(sdk.CreateImageEditMultipartBodyQualityAuto)
	}
	if size == "" {
		size = string(sdk.ImageSize1024X1024)
	}

	start := time.Now()
	model := t.config.Tools.ImageEdit.Model
	path, err := t.imageService.EditImage(ctx, model, prompt, image, quality, size)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "ImageEdit",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	return &domain.ToolExecutionResult{
		ToolName:  "ImageEdit",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"path":    path,
			"image":   image,
			"prompt":  prompt,
			"quality": quality,
			"size":    size,
		},
	}, nil
}

// IsEnabled returns whether the tool is enabled
func (t *ImageEditTool) IsEnabled() bool {
	return t.config.Tools.ImageEdit.Enabled &&
		t.config.Tools.ImageEdit.Model != "" &&
		t.imageService != nil
}

// FormatPreview formats the result for display preview
func (t *ImageEditTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Image edit failed"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Image edited"
	}
	path, _ := data["path"].(string)
	return fmt.Sprintf("Image saved to %s", path)
}

// FormatForLLM formats the result for LLM consumption
func (t *ImageEditTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Error: no result"
	}
	if !result.Success {
		return fmt.Sprintf("Image edit failed: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Image edited"
	}
	path, _ := data["path"].(string)
	quality, _ := data["quality"].(string)
	size, _ := data["size"].(string)
	formatter := domain.NewBaseFormatter("ImageEdit")
	return formatter.FormatExpanded(result, fmt.Sprintf("Image saved to %s (quality: %s, size: %s)", path, quality, size))
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *ImageEditTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *ImageEditTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *ImageEditTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
