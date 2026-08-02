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

// ImageVariationTool creates a variation of an existing image with the
// configured image model and saves the result to a local PNG. The chat model
// calls it with a local image path; the request goes straight to
// /v1/images/variations, independent of the model selected for the session.
type ImageVariationTool struct {
	config       *config.Config
	imageService domain.ImageService
}

// NewImageVariationTool creates a new ImageVariation tool
func NewImageVariationTool(cfg *config.Config, imageService domain.ImageService) *ImageVariationTool {
	return &ImageVariationTool{
		config:       cfg,
		imageService: imageService,
	}
}

// Definition returns the tool definition for ImageVariation
func (t *ImageVariationTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.ImageVariation.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ImageVariation",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"image": map[string]any{
						"type":        "string",
						"description": "Local file path of the image to base the variation on",
					},
					"size": map[string]any{
						"type":        "string",
						"enum":        imageSizes,
						"description": "Image size. Always use '1024x1024' unless the user explicitly asks for a larger or differently shaped image",
						"default":     string(sdk.ImageSize1024X1024),
					},
				},
				"required":             []string{"image"},
				"additionalProperties": false,
			},
		},
	}
}

// Validate validates ImageVariation arguments
func (t *ImageVariationTool) Validate(args map[string]any) error {
	image, ok := args["image"].(string)
	if !ok || image == "" {
		return fmt.Errorf("image is required and must be a non-empty file path")
	}
	if !t.imageService.IsImageFile(image) {
		return fmt.Errorf("image must point to a supported image file (png, jpg, jpeg, gif, webp)")
	}

	if raw, ok := args["size"]; ok {
		size, ok := raw.(string)
		if !ok || !slices.Contains(imageSizes, size) {
			return fmt.Errorf("size must be one of: 1024x1024, 1536x1024, 1024x1536")
		}
	}

	return nil
}

// Execute executes the ImageVariation tool
func (t *ImageVariationTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	image, _ := args["image"].(string)
	size, _ := args["size"].(string)

	if size == "" {
		size = string(sdk.ImageSize1024X1024)
	}

	start := time.Now()
	model := t.config.Tools.ImageVariation.Model
	path, err := t.imageService.CreateImageVariation(ctx, model, image, size)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "ImageVariation",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	return &domain.ToolExecutionResult{
		ToolName:  "ImageVariation",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"path":  path,
			"image": image,
			"size":  size,
		},
	}, nil
}

// IsEnabled returns whether the tool is enabled
func (t *ImageVariationTool) IsEnabled() bool {
	return t.config.Tools.ImageVariation.Enabled &&
		t.config.Tools.ImageVariation.Model != "" &&
		t.imageService != nil
}

// FormatPreview formats the result for display preview
func (t *ImageVariationTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Image variation failed"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Image variation created"
	}
	path, _ := data["path"].(string)
	return fmt.Sprintf("Image saved to %s", path)
}

// FormatForLLM formats the result for LLM consumption
func (t *ImageVariationTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Error: no result"
	}
	if !result.Success {
		return fmt.Sprintf("Image variation failed: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Image variation created"
	}
	path, _ := data["path"].(string)
	size, _ := data["size"].(string)
	formatter := domain.NewBaseFormatter("ImageVariation")
	return formatter.FormatExpanded(result, fmt.Sprintf("Image saved to %s (size: %s)", path, size))
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *ImageVariationTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *ImageVariationTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *ImageVariationTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
