package cmd

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// conversationWithToolImage is a user->assistant(tool_call)->tool exchange
// where the tool returned an image.
func conversationWithToolImage() []ConversationMessage {
	return []ConversationMessage{
		{Role: "user", Content: "what's on screen?"},
		{Role: "tool", Content: "frame retrieved", ToolCallID: "call_1",
			Images: []domain.ImageAttachment{{Data: "aW1n", MimeType: "image/jpeg", DisplayName: "frame-screen"}}},
	}
}

func TestBuildSDKMessagesHoistsToolImages(t *testing.T) {
	session := &AgentSession{
		config:       config.DefaultConfig(),
		model:        "anthropic/claude-sonnet-5",
		conversation: conversationWithToolImage(),
	}

	msgs := session.buildSDKMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected user + tool + hoisted image message, got %d", len(msgs))
	}

	if _, err := msgs[1].Content.AsMessageContent0(); err != nil {
		t.Errorf("tool message must stay text-only: %v", err)
	}

	parts, err := msgs[2].Content.AsMessageContent1()
	if err != nil {
		t.Fatalf("follow-up user message should be multipart: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected lead text + image part, got %d parts", len(parts))
	}
	if ip, err := parts[1].AsImageContentPart(); err != nil || string(ip.Type) != "image_url" {
		t.Errorf("second part should be an image for a vision model (type %q, err %v)", ip.Type, err)
	}
}

func TestBuildSDKMessagesTextOnlyModelGetsAnnotationText(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.Vision.TextOnlyModels = []string{"deepseek"}

	annotator := &domainmocks.FakeImageAnnotator{}
	annotator.AnnotateImageReturns(&domain.ImageAnnotation{Summary: "Login form"}, nil)

	session := &AgentSession{
		config:       cfg,
		model:        "deepseek/deepseek-chat",
		annotator:    annotator,
		conversation: conversationWithToolImage(),
	}

	msgs := session.buildSDKMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	parts, err := msgs[2].Content.AsMessageContent1()
	if err != nil {
		t.Fatalf("follow-up message should be multipart: %v", err)
	}
	for i, part := range parts {
		if ip, err := part.AsImageContentPart(); err == nil && string(ip.Type) == "image_url" {
			t.Errorf("part %d is an image; text-only models must receive no base64", i)
		}
	}
	text, err := parts[1].AsTextContentPart()
	if err != nil {
		t.Fatalf("expected text part: %v", err)
	}
	if want := "Frame summary: Login form"; text.Text != want {
		t.Errorf("annotation text = %q, want %q", text.Text, want)
	}
}

func TestBuildSDKMessagesTextOnlyWithoutAnnotatorGetsOmissionNote(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.Vision.TextOnlyModels = []string{"deepseek"}

	session := &AgentSession{
		config:       cfg,
		model:        "deepseek/deepseek-chat",
		conversation: conversationWithToolImage(),
	}

	msgs := session.buildSDKMessages()
	parts, err := msgs[2].Content.AsMessageContent1()
	if err != nil {
		t.Fatalf("follow-up message should be multipart: %v", err)
	}
	text, err := parts[1].AsTextContentPart()
	if err != nil {
		t.Fatalf("expected text part: %v", err)
	}
	if want := "[image omitted: model has no vision; configure vision.annotator for text descriptions]"; text.Text != want {
		t.Errorf("omission note = %q, want %q", text.Text, want)
	}
}
