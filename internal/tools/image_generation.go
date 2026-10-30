package tools

import (
	"context"
	"fmt"
	"slices"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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

// Manifest returns the tool's manifest with its configured require_approval.
func (t *ImageGenerationTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolImageGeneration).WithRequireApproval(t.config.Tools.ImageGeneration.RequireApproval)
}

// Definition returns the tool definition for ImageGeneration
func (t *ImageGenerationTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
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
			ToolName:  ToolImageGeneration,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolImageGeneration,
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
	return formatSavedResult(result, agentdomain.FormatterShort, imageGenerationFile)
}

// FormatForLLM formats the result for LLM consumption
func (t *ImageGenerationTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	return formatSavedResult(result, agentdomain.FormatterLLM, imageGenerationFile)
}

// FormatResult formats the result based on the requested format type
func (t *ImageGenerationTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	return formatSavedResult(result, formatType, imageGenerationFile)
}

// imageGenerationFile describes the file the ImageGeneration tool saves.
var imageGenerationFile = savedFile{
	tool:    ToolImageGeneration,
	noun:    "Image",
	failure: "Image generation failed",
	done:    "Image generated",
	details: imageDetails,
}
