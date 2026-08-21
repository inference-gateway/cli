package conversation

import (
	"errors"
	"testing"

	assert "github.com/stretchr/testify/assert"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"
)

func TestHTTPModelService_ValidateModel(t *testing.T) {
	tests := []struct {
		name         string
		modelID      string
		cachedModels []string
		fetchedIDs   []string
		listErr      error
		wantErr      string
	}{
		{
			name:    "empty model id",
			modelID: "",
			wantErr: "model ID cannot be empty",
		},
		{
			name:         "cached hit",
			modelID:      "openai/gpt-4o",
			cachedModels: []string{"openai/gpt-4o", "anthropic/claude-sonnet-5"},
		},
		{
			name:         "cached miss",
			modelID:      "openai/gpt-5",
			cachedModels: []string{"openai/gpt-4o"},
			wantErr:      "model 'openai/gpt-5' is not available",
		},
		{
			name:       "fetched hit",
			modelID:    "openai/gpt-4o",
			fetchedIDs: []string{"openai/gpt-4o"},
		},
		{
			name:       "fetched miss",
			modelID:    "openai/gpt-5",
			fetchedIDs: []string{"openai/gpt-4o"},
			wantErr:    "model 'openai/gpt-5' is not available",
		},
		{
			name:    "fetch error with valid format is accepted",
			modelID: "openai/gpt-4o",
			listErr: errors.New("gateway down"),
		},
		{
			name:    "fetch error with invalid format (no slash)",
			modelID: "gpt-4o",
			listErr: errors.New("gateway down"),
			wantErr: "invalid model ID format: gpt-4o",
		},
		{
			name:    "fetch error with slash but too short",
			modelID: "a/b",
			listErr: errors.New("gateway down"),
			wantErr: "invalid model ID format: a/b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &sdkmocks.FakeClient{}
			if tt.listErr != nil {
				fake.ListModelsReturns(nil, tt.listErr)
			} else {
				data := make([]sdk.Model, 0, len(tt.fetchedIDs))
				for _, id := range tt.fetchedIDs {
					data = append(data, sdk.Model{ID: id})
				}
				fake.ListModelsReturns(&sdk.ListModelsResponse{Object: "list", Data: data}, nil)
			}

			svc := NewHTTPModelService(fake)
			svc.models = tt.cachedModels

			err := svc.ValidateModel(tt.modelID)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}

			if len(tt.cachedModels) > 0 || tt.modelID == "" {
				assert.Zero(t, fake.ListModelsCallCount(), "cache/empty-id paths must not hit the gateway")
			}
		})
	}
}

func TestHTTPModelService_SelectModel(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		wantErr bool
	}{
		{name: "valid cached model is selected", modelID: "openai/gpt-4o"},
		{name: "invalid model leaves current empty", modelID: "openai/nope", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewHTTPModelService(&sdkmocks.FakeClient{})
			svc.models = []string{"openai/gpt-4o"}

			err := svc.SelectModel(tt.modelID)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, svc.GetCurrentModel())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.modelID, svc.GetCurrentModel())
			}
		})
	}
}

func TestHTTPModelService_IsModelAvailable(t *testing.T) {
	svc := NewHTTPModelService(&sdkmocks.FakeClient{})
	svc.models = []string{"openai/gpt-4o"}

	assert.True(t, svc.IsModelAvailable("openai/gpt-4o"))
	assert.False(t, svc.IsModelAvailable("openai/gpt-5"))
}
