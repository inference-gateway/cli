package adapters

import (
	"context"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// SDKClientAdapter adapts sdk.Client to domain.SDKClient interface
type SDKClientAdapter struct {
	client sdk.Client
}

// NewSDKClientAdapter creates a new SDK client adapter
func NewSDKClientAdapter(client sdk.Client) domain.SDKClient {
	return &SDKClientAdapter{
		client: client,
	}
}

// WithOptions wraps the SDK client's WithOptions method
func (a *SDKClientAdapter) WithOptions(opts *sdk.CreateChatCompletionRequest) domain.SDKClient {
	return &SDKClientAdapter{
		client: a.client.WithOptions(opts),
	}
}

// WithMiddlewareOptions wraps the SDK client's WithMiddlewareOptions method
func (a *SDKClientAdapter) WithMiddlewareOptions(opts *sdk.MiddlewareOptions) domain.SDKClient {
	return &SDKClientAdapter{
		client: a.client.WithMiddlewareOptions(opts),
	}
}

// WithTools wraps the SDK client's WithTools method
func (a *SDKClientAdapter) WithTools(tools *[]sdk.ChatCompletionTool) domain.SDKClient {
	return &SDKClientAdapter{
		client: a.client.WithTools(tools),
	}
}

// GenerateContent wraps the SDK client's GenerateContent method
func (a *SDKClientAdapter) GenerateContent(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (*sdk.CreateChatCompletionResponse, error) {
	return a.client.GenerateContent(ctx, provider, model, messages)
}

// GenerateContentStream wraps the SDK client's GenerateContentStream method
func (a *SDKClientAdapter) GenerateContentStream(ctx context.Context, provider sdk.Provider, model string, messages []sdk.Message) (<-chan sdk.SSEvent, error) {
	return a.client.GenerateContentStream(ctx, provider, model, messages)
}
