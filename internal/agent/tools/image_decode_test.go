package tools

import (
	"context"
	"errors"
	"testing"

	assert "github.com/stretchr/testify/assert"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func newImageDecodeTestTool(annotator domain.ImageAnnotator, images domain.ImageService) *ImageDecodeTool {
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.Vision.Annotator.Enabled = true
	cfg.Vision.Annotator.Model = "ollama/qwen3-vl:2b"
	return NewImageDecodeTool(cfg, images, annotator)
}

func TestImageDecodeIsEnabled(t *testing.T) {
	images := &domainmocks.FakeImageService{}
	annotator := &domainmocks.FakeImageAnnotator{}

	assert.True(t, newImageDecodeTestTool(annotator, images).IsEnabled())

	disabled := newImageDecodeTestTool(annotator, images)
	disabled.config.Vision.Annotator.Enabled = false
	assert.False(t, disabled.IsEnabled())

	assert.False(t, NewImageDecodeTool(config.DefaultConfig(), images, nil).IsEnabled())
}

func TestImageDecodeValidate(t *testing.T) {
	tool := newImageDecodeTestTool(&domainmocks.FakeImageAnnotator{}, &domainmocks.FakeImageService{})
	assert.Error(t, tool.Validate(map[string]any{}))
	assert.NoError(t, tool.Validate(map[string]any{"image": "shot.png"}))
}

func TestImageDecodeExecute(t *testing.T) {
	t.Run("not an image file", func(t *testing.T) {
		images := &domainmocks.FakeImageService{}
		images.IsImageFileReturns(false)
		tool := newImageDecodeTestTool(&domainmocks.FakeImageAnnotator{}, images)

		result, err := tool.Execute(context.Background(), map[string]any{"image": "notes.txt"})
		assert.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.Error, "not a readable image file")
	})

	t.Run("read failure", func(t *testing.T) {
		images := &domainmocks.FakeImageService{}
		images.IsImageFileReturns(true)
		images.ReadImageFromFileReturns(nil, errors.New("no such file"))
		tool := newImageDecodeTestTool(&domainmocks.FakeImageAnnotator{}, images)

		result, err := tool.Execute(context.Background(), map[string]any{"image": "gone.png"})
		assert.NoError(t, err)
		assert.False(t, result.Success)
	})

	t.Run("success with prompt pass-through", func(t *testing.T) {
		images := &domainmocks.FakeImageService{}
		images.IsImageFileReturns(true)
		images.ReadImageFromFileReturns(&domain.ImageAttachment{Data: "aW1n", MimeType: "image/png", Filename: "shot.png"}, nil)

		annotator := &domainmocks.FakeImageAnnotator{}
		annotator.AnnotateImageReturns(&domain.ImageAnnotation{
			Summary:  "A red button",
			Elements: []domain.AnnotatedElement{{Index: 1, Label: "button", BBox: [4]int{1, 2, 3, 4}}},
		}, nil)

		tool := newImageDecodeTestTool(annotator, images)
		result, err := tool.Execute(context.Background(), map[string]any{"image": "shot.png", "prompt": "what color is the button?"})
		assert.NoError(t, err)
		assert.True(t, result.Success)
		assert.Empty(t, result.Images, "ImageDecode is a description tool; it attaches nothing")

		_, img, opts := annotator.AnnotateImageArgsForCall(0)
		assert.Equal(t, "shot.png", img.SourcePath)
		assert.Contains(t, opts.Prompt, "what color is the button?")

		text := tool.FormatForLLM(result)
		assert.Contains(t, text, "Frame summary: A red button")
		assert.Contains(t, text, "1. button")
	})

	t.Run("annotator failure fails the call", func(t *testing.T) {
		images := &domainmocks.FakeImageService{}
		images.IsImageFileReturns(true)
		images.ReadImageFromFileReturns(&domain.ImageAttachment{Data: "aW1n", MimeType: "image/png"}, nil)

		annotator := &domainmocks.FakeImageAnnotator{}
		annotator.AnnotateImageReturns(nil, errors.New("model unreachable"))

		tool := newImageDecodeTestTool(annotator, images)
		result, err := tool.Execute(context.Background(), map[string]any{"image": "shot.png"})
		assert.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.Error, "annotation failed")
	})
}
