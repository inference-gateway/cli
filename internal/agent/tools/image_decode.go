package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ImageDecodeTool describes a local image file as text (scene summary +
// classified element list) via the configured vision annotator, making
// text-only session models effectively vision-capable on demand.
type ImageDecodeTool struct {
	config       *config.Config
	imageService domain.ImageService
	annotator    domain.ImageAnnotator
}

// NewImageDecodeTool creates a new ImageDecode tool
func NewImageDecodeTool(cfg *config.Config, imageService domain.ImageService, annotator domain.ImageAnnotator) *ImageDecodeTool {
	return &ImageDecodeTool{
		config:       cfg,
		imageService: imageService,
		annotator:    annotator,
	}
}

// Definition returns the tool definition for the LLM
func (t *ImageDecodeTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.ImageDecode.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "ImageDecode",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"image": map[string]any{
						"type":        "string",
						"description": "Path to a local image file (png, jpg, jpeg, gif, webp)",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "Optional question about the image; the summary answers it",
					},
				},
				"required": []string{"image"},
			},
		},
	}
}

// Execute annotates the image file and returns the text description
func (t *ImageDecodeTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	fail := func(msg string) (*domain.ToolExecutionResult, error) {
		return &domain.ToolExecutionResult{
			ToolName:  "ImageDecode",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     msg,
		}, nil
	}

	path, _ := args["image"].(string)

	attachment, err := t.imageService.ReadImageFromFile(path)
	if err != nil {
		return fail(err.Error())
	}
	attachment.SourcePath = strings.TrimPrefix(path, "file://")

	width, height := imageDimensions(attachment.Data)

	prompt := t.config.Prompts.Vision.Annotator.SceneSystemPrompt
	if question, _ := args["prompt"].(string); strings.TrimSpace(question) != "" {
		prompt += "\nAnswer this question about the image in the summary: " + question
	}

	annotation, err := t.annotator.AnnotateImage(ctx, *attachment, domain.AnnotateOptions{
		Prompt: prompt,
		Width:  width,
		Height: height,
	})
	if err != nil {
		return fail(fmt.Sprintf("annotation failed: %v", err))
	}

	return &domain.ToolExecutionResult{
		ToolName:  "ImageDecode",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: domain.FrameToolResult{
			Source:     path,
			Width:      width,
			Height:     height,
			Annotated:  true,
			Annotation: annotation,
		},
	}, nil
}

// imageDimensions sniffs pixel dimensions from base64 image data; zero values
// mean unknown (the annotator prompt then omits them).
func imageDimensions(b64 string) (int, int) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return 0, 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// Validate checks if the tool arguments are valid
func (t *ImageDecodeTool) Validate(args map[string]any) error {
	if path, _ := args["image"].(string); strings.TrimSpace(path) == "" {
		return fmt.Errorf("image is required")
	}
	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *ImageDecodeTool) IsEnabled() bool {
	return t.annotator != nil && t.imageService != nil && t.config.Vision.AnnotatorReady()
}

// FormatResult formats tool execution results for different contexts
func (t *ImageDecodeTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *ImageDecodeTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to decode image"
	}
	if data, ok := result.Data.(domain.FrameToolResult); ok && data.Annotation != nil {
		return fmt.Sprintf("Decoded image: %d element(s)", len(data.Annotation.Elements))
	}
	return "Image decoded"
}

// FormatForLLM formats the result for LLM consumption
func (t *ImageDecodeTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	if data, ok := result.Data.(domain.FrameToolResult); ok && data.Annotation != nil {
		return domain.AnnotationText(data.Annotation)
	}
	return "Image decoded."
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *ImageDecodeTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *ImageDecodeTool) ShouldAlwaysExpand() bool {
	return false
}
