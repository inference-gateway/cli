package vlm

import (
	"context"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"
)

func gatewayTestConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Vision.Annotator.Enabled = true
	cfg.Vision.Annotator.Model = "ollama/qwen3-vl:4b"
	cfg.Prompts = *config.DefaultPromptsConfig()
	return cfg
}

func TestGatewayAnnotateImage(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.WithOptionsReturns(client)
	client.WithMiddlewareOptionsReturns(client)
	client.GenerateContentReturns(&sdk.CreateChatCompletionResponse{
		Choices: []sdk.ChatCompletionChoice{
			{Message: sdk.Message{Content: sdk.NewMessageContent(`{"summary":"A forklift","elements":[{"index":1,"label":"forklift","bbox":[60,260,240,420]}]}`)}},
		},
	}, nil)

	a := NewGatewayAnnotator(client, gatewayTestConfig())
	img := agentdomain.ImageAttachment{Data: "aW1n", MimeType: "image/jpeg"}
	got, err := a.AnnotateImage(context.Background(), img, agentdomain.AnnotateOptions{Width: 640, Height: 480})
	if err != nil {
		t.Fatalf("AnnotateImage: %v", err)
	}
	if got.Summary != "A forklift" || len(got.Elements) != 1 {
		t.Errorf("unexpected annotation: %+v", got)
	}

	_, provider, model, messages := client.GenerateContentArgsForCall(0)
	if provider != sdk.Provider("ollama") || model != "qwen3-vl:4b" {
		t.Errorf("provider/model = %s/%s", provider, model)
	}
	if len(messages) != 2 {
		t.Fatalf("expected system+user messages, got %d", len(messages))
	}
	parts, err := messages[1].Content.AsMessageContent1()
	if err != nil {
		t.Fatalf("user message should be multipart: %v", err)
	}
	if len(parts) != 2 {
		t.Errorf("expected text+image parts, got %d", len(parts))
	}
}

func TestGatewayAnnotateImageBadModelFormat(t *testing.T) {
	cfg := gatewayTestConfig()
	cfg.Vision.Annotator.Model = "no-slash"
	a := NewGatewayAnnotator(&sdkmocks.FakeClient{}, cfg)
	_, err := a.AnnotateImage(context.Background(), agentdomain.ImageAttachment{Data: "aW1n", MimeType: "image/png"}, agentdomain.AnnotateOptions{})
	if err == nil || !strings.Contains(err.Error(), "provider/model") {
		t.Fatalf("expected provider/model error, got %v", err)
	}
}

func TestGatewayAnnotateImageGarbageDegrades(t *testing.T) {
	client := &sdkmocks.FakeClient{}
	client.WithOptionsReturns(client)
	client.WithMiddlewareOptionsReturns(client)
	client.GenerateContentReturns(&sdk.CreateChatCompletionResponse{
		Choices: []sdk.ChatCompletionChoice{
			{Message: sdk.Message{Content: sdk.NewMessageContent("I see a login form with two fields.")}},
		},
	}, nil)

	a := NewGatewayAnnotator(client, gatewayTestConfig())
	got, err := a.AnnotateImage(context.Background(), agentdomain.ImageAttachment{Data: "aW1n", MimeType: "image/png"}, agentdomain.AnnotateOptions{})
	if err != nil {
		t.Fatalf("AnnotateImage: %v", err)
	}
	if got.Summary != "I see a login form with two fields." || len(got.Elements) != 0 {
		t.Errorf("expected degrade to summary-only, got %+v", got)
	}
}
