package tools

import (
	"context"
	"fmt"
	"testing"

	assert "github.com/stretchr/testify/assert"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	config "github.com/inference-gateway/cli/config"
)

func newTestImageEditTool(imageService *agentdomainmocks.FakeImageService) *ImageEditTool {
	return NewImageEditTool(config.DefaultConfig(), imageService)
}

func TestImageEditTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name         string
		configEnable bool
		model        string
		expected     bool
	}{
		{"enabled with configured model", true, "openai/gpt-image-2", true},
		{"no model configured", true, "", false},
		{"disabled in config", false, "openai/gpt-image-2", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Tools.ImageEdit.Enabled = tt.configEnable
			cfg.Tools.ImageEdit.Model = tt.model
			tool := NewImageEditTool(cfg, &agentdomainmocks.FakeImageService{})

			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestImageEditTool_Validate(t *testing.T) {
	imageService := &agentdomainmocks.FakeImageService{}
	imageService.IsImageFileReturns(true)
	tool := newTestImageEditTool(imageService)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"image and prompt", map[string]any{"image": "input.png", "prompt": "make it blue"}, false},
		{"image prompt quality and size", map[string]any{"image": "input.png", "prompt": "make it blue", "quality": "high", "size": "1536x1024"}, false},
		{"missing image", map[string]any{"prompt": "make it blue"}, true},
		{"empty image", map[string]any{"image": "", "prompt": "make it blue"}, true},
		{"missing prompt", map[string]any{"image": "input.png"}, true},
		{"empty prompt", map[string]any{"image": "input.png", "prompt": ""}, true},
		{"bad quality", map[string]any{"image": "input.png", "prompt": "make it blue", "quality": "ultra"}, true},
		{"bad size", map[string]any{"image": "input.png", "prompt": "make it blue", "size": "9999x9999"}, true},
		{"png mask", map[string]any{"image": "input.png", "prompt": "make it blue", "mask": "mask.png"}, false},
		{"uppercase png mask", map[string]any{"image": "input.png", "prompt": "make it blue", "mask": "MASK.PNG"}, false},
		{"non-png mask", map[string]any{"image": "input.png", "prompt": "make it blue", "mask": "mask.jpg"}, true},
		{"empty mask", map[string]any{"image": "input.png", "prompt": "make it blue", "mask": ""}, true},
		{"non-string mask", map[string]any{"image": "input.png", "prompt": "make it blue", "mask": 42}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			assert.Equal(t, tt.wantErr, err != nil, "err = %v", err)
		})
	}
}

func TestImageEditTool_Validate_nonImageFile(t *testing.T) {
	imageService := &agentdomainmocks.FakeImageService{}
	imageService.IsImageFileReturns(false)
	tool := newTestImageEditTool(imageService)

	err := tool.Validate(map[string]any{"image": "notes.txt", "prompt": "make it blue"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "supported image file")
}

func TestImageEditTool_Execute(t *testing.T) {
	t.Run("defaults to auto quality and the configured model", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.IsImageFileReturns(true)
		imageService.EditImageReturns("image-1.png", nil)
		tool := newTestImageEditTool(imageService)

		result, err := tool.Execute(context.Background(), map[string]any{"image": "input.png", "prompt": "make it blue"})

		assert.NoError(t, err)
		assert.True(t, result.Success)
		_, model, prompt, image, quality, size, mask := imageService.EditImageArgsForCall(0)
		assert.Equal(t, "", mask)
		assert.Equal(t, "openai/gpt-image-2", model)
		assert.Equal(t, "make it blue", prompt)
		assert.Equal(t, "input.png", image)
		assert.Equal(t, "auto", quality)
		assert.Equal(t, "1024x1024", size)
	})

	t.Run("passes through an explicit quality and size", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.IsImageFileReturns(true)
		imageService.EditImageReturns("image-1.png", nil)
		tool := newTestImageEditTool(imageService)

		_, err := tool.Execute(context.Background(), map[string]any{
			"image": "input.png", "prompt": "make it blue", "quality": "high", "size": "1536x1024",
		})

		assert.NoError(t, err)
		_, _, _, _, quality, size, mask := imageService.EditImageArgsForCall(0)
		assert.Equal(t, "", mask)
		assert.Equal(t, "high", quality)
		assert.Equal(t, "1536x1024", size)
	})

	t.Run("passes through the mask path", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.IsImageFileReturns(true)
		imageService.EditImageReturns("image-1.png", nil)
		tool := newTestImageEditTool(imageService)

		_, err := tool.Execute(context.Background(), map[string]any{
			"image": "input.png", "prompt": "make it blue", "mask": "mask.png",
		})

		assert.NoError(t, err)
		_, _, _, _, _, _, mask := imageService.EditImageArgsForCall(0)
		assert.Equal(t, "mask.png", mask)
	})

	t.Run("edit failure is a failed result, not an error", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.IsImageFileReturns(true)
		imageService.EditImageReturns("", fmt.Errorf("API error: 404"))
		tool := newTestImageEditTool(imageService)

		result, err := tool.Execute(context.Background(), map[string]any{"image": "input.png", "prompt": "make it blue"})

		assert.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.Error, "404")
	})

	t.Run("invalid args error out before calling the service", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.IsImageFileReturns(true)
		tool := newTestImageEditTool(imageService)

		_, err := tool.Execute(context.Background(), map[string]any{"quality": "low"})

		assert.Error(t, err)
		assert.Zero(t, imageService.EditImageCallCount())
	})
}
