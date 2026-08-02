package vlm

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// GatewayAnnotator annotates images with a one-off side-call to a
// vision-capable provider/model routed through the inference gateway - the
// same mechanism conversation-title generation uses.
type GatewayAnnotator struct {
	client sdk.Client
	cfg    *config.Config
}

// NewGatewayAnnotator creates a gateway-backed annotator.
func NewGatewayAnnotator(client sdk.Client, cfg *config.Config) *GatewayAnnotator {
	return &GatewayAnnotator{client: client, cfg: cfg}
}

// AnnotateImage sends the image to the configured vision model and parses the
// JSON reply.
func (a *GatewayAnnotator) AnnotateImage(ctx context.Context, img domain.ImageAttachment, opts domain.AnnotateOptions) (*domain.ImageAnnotation, error) {
	if a.client == nil {
		return nil, fmt.Errorf("AI client not available")
	}

	model := a.cfg.Vision.Annotator.Model
	slashIndex := strings.Index(model, "/")
	if slashIndex == -1 {
		return nil, fmt.Errorf("invalid vision.annotator.model %q: expected 'provider/model'", model)
	}
	provider := model[:slashIndex]
	modelName := strings.TrimPrefix(model, provider+"/")

	task := strings.TrimSpace(opts.Prompt)
	if task == "" {
		task = a.cfg.Prompts.Vision.Annotator.SceneSystemPrompt
	}
	systemPrompt := task + "\n" + fmt.Sprintf(jsonContract, "bbox coordinates are pixels in the image.")

	userText := "Annotate this image. JSON only."
	if opts.Width > 0 && opts.Height > 0 {
		userText = fmt.Sprintf("Annotate this %dx%d image. JSON only.", opts.Width, opts.Height)
	}
	textPart, err := sdk.NewTextContentPart(userText)
	if err != nil {
		return nil, fmt.Errorf("creating text content part: %w", err)
	}

	dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Data)
	imagePart, err := sdk.NewImageContentPart(dataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating image content part: %w", err)
	}

	maxTokens := a.cfg.Vision.Annotator.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	messages := []sdk.Message{
		{Role: sdk.System, Content: sdk.NewMessageContent(systemPrompt)},
		{Role: sdk.User, Content: sdk.NewMessageContent([]sdk.ContentPart{textPart, imagePart})},
	}

	response, err := a.client.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens: &maxTokens,
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
		}).
		GenerateContent(ctx, sdk.Provider(provider), modelName, messages)
	if err != nil {
		return nil, fmt.Errorf("image annotation failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no annotation generated")
	}

	content, err := response.Choices[0].Message.Content.AsMessageContent0()
	if err != nil {
		return nil, fmt.Errorf("failed to extract annotation content: %w", err)
	}

	return parseAnnotation(content), nil
}
