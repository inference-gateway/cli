package tools

import (
	"context"
	"fmt"
	"testing"

	assert "github.com/stretchr/testify/assert"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	config "github.com/inference-gateway/cli/config"
)

func newTestImageVariationTool(imageService *agentdomainmocks.FakeImageService) *ImageVariationTool {
	return NewImageVariationTool(config.DefaultConfig(), imageService)
}

func TestImageVariationTool_IsEnabled(t *testing.T) {
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
			cfg.Tools.ImageVariation.Enabled = tt.configEnable
			cfg.Tools.ImageVariation.Model = tt.model
			tool := NewImageVariationTool(cfg, &agentdomainmocks.FakeImageService{})

			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestImageVariationTool_Validate(t *testing.T) {
	imageService := &agentdomainmocks.FakeImageService{}
	imageService.IsImageFileReturns(true)
	tool := newTestImageVariationTool(imageService)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"image only", map[string]any{"image": "input.png"}, false},
		{"image with size", map[string]any{"image": "input.png", "size": "1536x1024"}, false},
		{"missing image", map[string]any{}, true},
		{"empty image", map[string]any{"image": ""}, true},
		{"bad size", map[string]any{"image": "input.png", "size": "9999x9999"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			assert.Equal(t, tt.wantErr, err != nil, "err = %v", err)
		})
	}
}

func TestImageVariationTool_Validate_nonImageFile(t *testing.T) {
	imageService := &agentdomainmocks.FakeImageService{}
	imageService.IsImageFileReturns(false)
	tool := newTestImageVariationTool(imageService)

	err := tool.Validate(map[string]any{"image": "notes.txt"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "supported image file")
}

func TestImageVariationTool_Execute(t *testing.T) {
	t.Run("defaults to 1024x1024 and the configured model", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.IsImageFileReturns(true)
		imageService.CreateImageVariationReturns("image-1.png", nil)
		tool := newTestImageVariationTool(imageService)

		result, err := tool.Execute(context.Background(), map[string]any{"image": "input.png"})

		assert.NoError(t, err)
		assert.True(t, result.Success)
		_, model, image, size := imageService.CreateImageVariationArgsForCall(0)
		assert.Equal(t, "openai/gpt-image-2", model)
		assert.Equal(t, "input.png", image)
		assert.Equal(t, "1024x1024", size)
	})

	t.Run("passes through an explicit size", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.IsImageFileReturns(true)
		imageService.CreateImageVariationReturns("image-1.png", nil)
		tool := newTestImageVariationTool(imageService)

		_, err := tool.Execute(context.Background(), map[string]any{"image": "input.png", "size": "1536x1024"})

		assert.NoError(t, err)
		_, _, _, size := imageService.CreateImageVariationArgsForCall(0)
		assert.Equal(t, "1536x1024", size)
	})

	t.Run("variation failure is a failed result, not an error", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.IsImageFileReturns(true)
		imageService.CreateImageVariationReturns("", fmt.Errorf("API error: 404"))
		tool := newTestImageVariationTool(imageService)

		result, err := tool.Execute(context.Background(), map[string]any{"image": "input.png"})

		assert.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.Error, "404")
	})

	t.Run("invalid args error out before calling the service", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.IsImageFileReturns(true)
		tool := newTestImageVariationTool(imageService)

		_, err := tool.Execute(context.Background(), map[string]any{"size": "low"})

		assert.Error(t, err)
		assert.Zero(t, imageService.CreateImageVariationCallCount())
	})
}
