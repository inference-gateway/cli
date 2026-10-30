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

// ImageVariationTool creates a variation of an existing image with the
// configured image model and saves the result to a local PNG. The chat model
// calls it with a local image path; the request goes straight to
// /v1/images/edits, independent of the model selected for the session.
type ImageVariationTool struct {
	config       *config.Config
	imageService agentdomain.ImageService
}

// NewImageVariationTool creates a new ImageVariation tool
func NewImageVariationTool(cfg *config.Config, imageService agentdomain.ImageService) *ImageVariationTool {
	return &ImageVariationTool{
		config:       cfg,
		imageService: imageService,
	}
}

// Manifest returns the tool's manifest with its configured require_approval.
func (t *ImageVariationTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolImageVariation).WithRequireApproval(t.config.Tools.ImageVariation.RequireApproval)
}

// Definition returns the tool definition for ImageVariation
func (t *ImageVariationTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
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
func (t *ImageVariationTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
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
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolImageVariation,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolImageVariation,
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
func (t *ImageVariationTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	return formatSavedResult(result, agentdomain.FormatterShort, imageVariationFile)
}

// FormatForLLM formats the result for LLM consumption
func (t *ImageVariationTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	return formatSavedResult(result, agentdomain.FormatterLLM, imageVariationFile)
}

// FormatResult formats the result based on the requested format type
func (t *ImageVariationTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	return formatSavedResult(result, formatType, imageVariationFile)
}

// imageVariationFile describes the file the ImageVariation tool saves.
var imageVariationFile = savedFile{
	tool:    ToolImageVariation,
	noun:    "Image",
	failure: "Image variation failed",
	done:    "Image variation created",
	details: imageVariationDetails,
}

// imageVariationDetails renders the size an image variation reports.
func imageVariationDetails(data map[string]any) string {
	size, _ := data["size"].(string)
	return fmt.Sprintf(" (size: %s)", size)
}
