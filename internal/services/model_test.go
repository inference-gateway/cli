package services

import (
	"context"
	"testing"

	assert "github.com/stretchr/testify/assert"

	sdk "github.com/inference-gateway/sdk"

	models "github.com/inference-gateway/cli/internal/models"
	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"
)

// TestHTTPModelService_ListModelsPublishesMetadata verifies that a models
// fetch pushes the gateway-reported context windows and prices into the
// shared registries, keyed by full model id, and skips models without
// metadata.
func TestHTTPModelService_ListModelsPublishesMetadata(t *testing.T) {
	defer models.SetGatewayContextWindows(nil)
	defer setGatewayPricing(nil)

	cache := "0.00000025"
	fake := &sdkmocks.FakeClient{}
	fake.ListModelsReturns(&sdk.ListModelsResponse{
		Object: "list",
		Data: []sdk.Model{
			{
				ID:            "prov/metadata-model",
				ContextWindow: &sdk.ContextWindow{Tokens: 424242, Source: sdk.ContextWindowSourceProvider},
				Pricing: &sdk.Pricing{
					InputPerToken:     "0.0000025",
					OutputPerToken:    "0.00001",
					CacheReadPerToken: &cache,
					Currency:          "USD",
					Source:            sdk.PricingSourceProvider,
				},
			},
			{ID: "prov/bare-model"},
		},
	}, nil)

	svc := NewHTTPModelService(fake)
	ids, err := svc.ListModels(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, []string{"prov/metadata-model", "prov/bare-model"}, ids)

	window, known := models.LookupContextWindow("prov/metadata-model")
	assert.True(t, known)
	assert.Equal(t, 424242, window)

	price, ok := gatewayPriceFor("prov/metadata-model")
	assert.True(t, ok)
	assert.InDelta(t, 2.5, price.inputPerMTok, 1e-9)
	assert.InDelta(t, 10.0, price.outputPerMTok, 1e-9)
	if assert.NotNil(t, price.cacheReadPerMTok) {
		assert.InDelta(t, 0.25, *price.cacheReadPerMTok, 1e-9)
	}

	_, ok = gatewayPriceFor("prov/bare-model")
	assert.False(t, ok)
}

// TestHTTPModelService_ListModelsFiltersImageModels verifies that image-generation
// models (output modalities include "image" but not "text") are excluded from the
// selectable list; vision models (both "text" and "image") are kept.
func TestHTTPModelService_ListModelsFiltersImageModels(t *testing.T) {
	defer models.SetGatewayModalities(nil)

	imageMods := sdk.ModelModalities{
		Input:  []sdk.Modality{sdk.ModalityText, sdk.ModalityImage},
		Output: []sdk.Modality{sdk.ModalityImage},
	}
	textMods := sdk.ModelModalities{
		Input:  []sdk.Modality{sdk.ModalityText},
		Output: []sdk.Modality{sdk.ModalityText},
	}

	fake := &sdkmocks.FakeClient{}
	fake.ListModelsReturns(&sdk.ListModelsResponse{
		Object: "list",
		Data: []sdk.Model{
			{ID: "deepseek/deepseek-v4-flash", Modalities: &textMods},
			{ID: "openai/gpt-image-2", Modalities: &imageMods},
			{ID: "openai/dall-e-3", Modalities: &imageMods},
			{ID: "black-forest-labs/flux-1.1-pro", Modalities: &imageMods},
		},
	}, nil)

	svc := NewHTTPModelService(fake)
	ids, err := svc.ListModels(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, []string{"deepseek/deepseek-v4-flash"}, ids)
}

// TestHTTPModelService_ListModelsKeepsVisionModels verifies that models with
// both "text" and "image" modalities (vision-capable models) are NOT filtered
// out of the selectable list.
func TestHTTPModelService_ListModelsKeepsVisionModels(t *testing.T) {
	defer models.SetGatewayModalities(nil)

	textImageMods := sdk.ModelModalities{
		Input:  []sdk.Modality{sdk.ModalityText, sdk.ModalityImage},
		Output: []sdk.Modality{sdk.ModalityText},
	}
	textMods := sdk.ModelModalities{
		Input:  []sdk.Modality{sdk.ModalityText},
		Output: []sdk.Modality{sdk.ModalityText},
	}

	fake := &sdkmocks.FakeClient{}
	fake.ListModelsReturns(&sdk.ListModelsResponse{
		Object: "list",
		Data: []sdk.Model{
			{ID: "openai/gpt-4o", Modalities: &textImageMods},
			{ID: "anthropic/claude-sonnet-5", Modalities: &textImageMods},
			{ID: "deepseek/deepseek-v4-flash", Modalities: &textMods},
		},
	}, nil)

	svc := NewHTTPModelService(fake)
	ids, err := svc.ListModels(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, []string{"openai/gpt-4o", "anthropic/claude-sonnet-5", "deepseek/deepseek-v4-flash"}, ids)
}

// TestHTTPModelService_ListModelsWithoutMetadataKeepsFallbacks verifies that
// a metadata-less response (older gateway ignoring the include param) leaves
// the existing registries untouched instead of wiping them.
func TestHTTPModelService_ListModelsWithoutMetadataKeepsFallbacks(t *testing.T) {
	models.SetGatewayContextWindows(map[string]int{"prov/sentinel": 111})
	defer models.SetGatewayContextWindows(nil)

	fake := &sdkmocks.FakeClient{}
	fake.ListModelsReturns(&sdk.ListModelsResponse{
		Object: "list",
		Data:   []sdk.Model{{ID: "prov/bare-model"}},
	}, nil)

	_, err := NewHTTPModelService(fake).ListModels(context.Background())
	assert.NoError(t, err)

	window, known := models.LookupContextWindow("prov/sentinel")
	assert.True(t, known)
	assert.Equal(t, 111, window, "empty metadata must not replace the registry")
}
