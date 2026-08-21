package tools

import (
	"context"
	"fmt"
	"slices"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
)

// ImageGenerationTool generates an image with the configured image model and
// saves it to a local PNG. The chat model calls it with a plain prompt; the
// request goes straight to /v1/images/generations, independent of the model
// selected for the session.
type ImageGenerationTool struct {
	config       *config.Config
	imageService agentdomain.ImageService
}

var (
	imageQualities = []string{
		string(sdk.CreateImageRequestQualityLow),
		string(sdk.CreateImageRequestQualityMedium),
		string(sdk.CreateImageRequestQualityHigh),
	}
	imageSizes = []string{
		string(sdk.ImageSize1024X1024),
		string(sdk.ImageSize1536X1024),
		string(sdk.ImageSize1024X1536),
	}
)

// NewImageGenerationTool creates a new ImageGeneration tool
func NewImageGenerationTool(cfg *config.Config, imageService agentdomain.ImageService) *ImageGenerationTool {
	return &ImageGenerationTool{
		config:       cfg,
		imageService: imageService,
	}
}

// Definition returns the tool definition for ImageGeneration
func (t *ImageGenerationTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.ImageGeneration.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ImageGeneration",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "A text description of the desired image",
					},
					"quality": map[string]any{
						"type":        "string",
						"enum":        imageQualities,
						"description": "Image quality. Always use 'low' unless the user explicitly asks for higher quality - it is markedly cheaper and faster",
						"default":     string(sdk.CreateImageRequestQualityLow),
					},
					"size": map[string]any{
						"type":        "string",
						"enum":        imageSizes,
						"description": "Image size. Always use '1024x1024' unless the user explicitly asks for a larger or differently shaped image",
						"default":     string(sdk.ImageSize1024X1024),
					},
				},
				"required":             []string{"prompt"},
				"additionalProperties": false,
			},
		},
	}
}

// Validate validates ImageGeneration arguments
func (t *ImageGenerationTool) Validate(args map[string]any) error {
	prompt, ok := args["prompt"].(string)
	if !ok || prompt == "" {
		return fmt.Errorf("prompt is required and must be a non-empty string")
	}

	if raw, ok := args["quality"]; ok {
		quality, ok := raw.(string)
		if !ok || !slices.Contains(imageQualities, quality) {
			return fmt.Errorf("quality must be one of: low, medium, high")
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

// Execute executes the ImageGeneration tool
func (t *ImageGenerationTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	prompt, _ := args["prompt"].(string)
	quality, _ := args["quality"].(string)
	size, _ := args["size"].(string)

	if quality == "" {
		quality = string(sdk.CreateImageRequestQualityLow)
	}
	if size == "" {
		size = string(sdk.ImageSize1024X1024)
	}

	start := time.Now()
	model := t.config.Tools.ImageGeneration.Model
	path, err := t.imageService.GenerateImage(ctx, model, prompt, quality, size)
	if err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "ImageGeneration",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  "ImageGeneration",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"path":    path,
			"prompt":  prompt,
			"quality": quality,
			"size":    size,
		},
	}, nil
}

// IsEnabled returns whether the tool is enabled
func (t *ImageGenerationTool) IsEnabled() bool {
	return t.config.Tools.ImageGeneration.Enabled &&
		t.config.Tools.ImageGeneration.Model != "" &&
		t.imageService != nil
}

// FormatPreview formats the result for display preview
func (t *ImageGenerationTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Image generation failed"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Image generated"
	}
	path, _ := data["path"].(string)
	return fmt.Sprintf("Image saved to %s", path)
}

// FormatForLLM formats the result for LLM consumption
func (t *ImageGenerationTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Error: no result"
	}
	if !result.Success {
		return fmt.Sprintf("Image generation failed: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Image generated"
	}
	path, _ := data["path"].(string)
	quality, _ := data["quality"].(string)
	size, _ := data["size"].(string)
	formatter := agentinfra.NewBaseFormatter("ImageGeneration")
	return formatter.FormatExpanded(result, fmt.Sprintf("Image saved to %s (quality: %s, size: %s)", path, quality, size))
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *ImageGenerationTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *ImageGenerationTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *ImageGenerationTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
