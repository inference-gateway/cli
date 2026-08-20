package tools

import (
	"context"
	"fmt"
	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	"testing"

	assert "github.com/stretchr/testify/assert"

	config "github.com/inference-gateway/cli/config"
)

func newTestImageTool(imageService *agentdomainmocks.FakeImageService) *ImageGenerationTool {
	return NewImageGenerationTool(config.DefaultConfig(), imageService)
}

func TestImageGenerationTool_IsEnabled(t *testing.T) {
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
			cfg.Tools.ImageGeneration.Enabled = tt.configEnable
			cfg.Tools.ImageGeneration.Model = tt.model
			tool := NewImageGenerationTool(cfg, &agentdomainmocks.FakeImageService{})

			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestImageGenerationTool_Validate(t *testing.T) {
	tool := newTestImageTool(&agentdomainmocks.FakeImageService{})

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"prompt only", map[string]any{"prompt": "a cat"}, false},
		{"prompt with quality and size", map[string]any{"prompt": "a cat", "quality": "high", "size": "1536x1024"}, false},
		{"missing prompt", map[string]any{}, true},
		{"empty prompt", map[string]any{"prompt": ""}, true},
		{"bad quality", map[string]any{"prompt": "a cat", "quality": "ultra"}, true},
		{"unadvertised sdk quality", map[string]any{"prompt": "a cat", "quality": "hd"}, true},
		{"bad size", map[string]any{"prompt": "a cat", "size": "9999x9999"}, true},
		{"unadvertised sdk size", map[string]any{"prompt": "a cat", "size": "256x256"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			assert.Equal(t, tt.wantErr, err != nil, "err = %v", err)
		})
	}
}

func TestImageGenerationTool_Execute(t *testing.T) {
	t.Run("defaults to the cheap tier and the configured model", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.GenerateImageReturns("image-1.png", nil)
		tool := newTestImageTool(imageService)

		result, err := tool.Execute(context.Background(), map[string]any{"prompt": "a cat"})

		assert.NoError(t, err)
		assert.True(t, result.Success)
		_, model, prompt, quality, size := imageService.GenerateImageArgsForCall(0)
		assert.Equal(t, "openai/gpt-image-2", model)
		assert.Equal(t, "a cat", prompt)
		assert.Equal(t, "low", quality)
		assert.Equal(t, "1024x1024", size)
	})

	t.Run("passes through an explicit quality and size", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.GenerateImageReturns("image-1.png", nil)
		tool := newTestImageTool(imageService)

		_, err := tool.Execute(context.Background(), map[string]any{
			"prompt": "a cat", "quality": "high", "size": "1536x1024",
		})

		assert.NoError(t, err)
		_, _, _, quality, size := imageService.GenerateImageArgsForCall(0)
		assert.Equal(t, "high", quality)
		assert.Equal(t, "1536x1024", size)
	})

	t.Run("generation failure is a failed result, not an error", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		imageService.GenerateImageReturns("", fmt.Errorf("API error: 404"))
		tool := newTestImageTool(imageService)

		result, err := tool.Execute(context.Background(), map[string]any{"prompt": "a cat"})

		assert.NoError(t, err)
		assert.False(t, result.Success)
		assert.Contains(t, result.Error, "404")
	})

	t.Run("invalid args error out before calling the service", func(t *testing.T) {
		imageService := &agentdomainmocks.FakeImageService{}
		tool := newTestImageTool(imageService)

		_, err := tool.Execute(context.Background(), map[string]any{"quality": "low"})

		assert.Error(t, err)
		assert.Zero(t, imageService.GenerateImageCallCount())
	})
}
