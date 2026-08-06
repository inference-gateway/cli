package cmd

import (
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	models "github.com/inference-gateway/cli/internal/models"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// Image files (both @refs and --files) become text path references — never
// inline base64 — matching the chat TUI behavior.
func TestExpandFileReferencesImagesBecomePathRefs(t *testing.T) {
	fileService := &mocks.FakeFileService{}
	fileService.ValidateFileReturns(nil)
	imageService := &mocks.FakeImageService{}
	imageService.IsImageFileReturns(true)

	session := &AgentSession{
		config:       config.DefaultConfig(),
		model:        "deepseek/deepseek-v4-flash",
		fileService:  fileService,
		imageService: imageService,
	}

	expanded, err := session.expandFileReferences("look at @photo.jpg", []string{"/data/upload.png"})
	if err != nil {
		t.Fatalf("expandFileReferences: %v", err)
	}
	for _, path := range []string{"photo.jpg", "/data/upload.png"} {
		want := domain.ImageFileRef(path, false)
		if !strings.Contains(expanded, want) {
			t.Errorf("expanded content missing ref for %s:\n%s", path, expanded)
		}
	}
	if fileService.ReadFileCallCount() != 0 {
		t.Error("image bytes must not be read/inlined")
	}
	if imageService.ReadImageFromFileCallCount() != 0 {
		t.Error("images must not be attached as base64")
	}
}

// conversationWithToolImage is a user->assistant(tool_call)->tool exchange
// where the tool returned an image.
func conversationWithToolImage() []ConversationMessage {
	return []ConversationMessage{
		{Role: "user", Content: "what's on screen?"},
		{Role: "tool", Content: "frame retrieved", ToolCallID: "call_1",
			Images: []domain.ImageAttachment{{Data: "aW1n", MimeType: "image/jpeg",
				DisplayName: "frame-screen", SourcePath: "/tmp/frame.jpg"}}},
	}
}

func TestBuildSDKMessagesHoistsToolImages(t *testing.T) {
	models.SetGatewayModalities(map[string]sdk.ModelModalities{
		"anthropic/claude-sonnet-5": {Input: []sdk.Modality{sdk.ModalityText, sdk.ModalityImage}, Output: []sdk.Modality{sdk.ModalityText}},
	})
	t.Cleanup(func() { models.SetGatewayModalities(nil) })

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
		t.Errorf("second part should be an image (type %q, err %v)", ip.Type, err)
	}
}

// A non-vision model must never receive raw image parts — the hoisted message
// carries text path notes pointing at ImageDecode instead.
func TestBuildSDKMessagesToolImagesNonVisionModel(t *testing.T) {
	session := &AgentSession{
		config:       config.DefaultConfig(),
		model:        "deepseek/deepseek-v4-flash",
		conversation: conversationWithToolImage(),
	}

	msgs := session.buildSDKMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected user + tool + hoisted note message, got %d", len(msgs))
	}

	parts, err := msgs[2].Content.AsMessageContent1()
	if err != nil {
		t.Fatalf("follow-up user message should be multipart: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected lead text + path note, got %d parts", len(parts))
	}
	note, err := parts[1].AsTextContentPart()
	if err != nil {
		t.Fatalf("second part should be text for a non-vision model: %v", err)
	}
	if !strings.Contains(note.Text, "/tmp/frame.jpg") || !strings.Contains(note.Text, "ImageDecode") {
		t.Errorf("note should point at the saved path and ImageDecode, got %q", note.Text)
	}
}

// Non-vision model + image with no on-disk path: nothing useful to hoist.
func TestBuildSDKMessagesToolImagesNonVisionNoPath(t *testing.T) {
	session := &AgentSession{
		config: config.DefaultConfig(),
		model:  "deepseek/deepseek-v4-flash",
		conversation: []ConversationMessage{
			{Role: "user", Content: "what's on screen?"},
			{Role: "tool", Content: "frame retrieved", ToolCallID: "call_1",
				Images: []domain.ImageAttachment{{Data: "aW1n", MimeType: "image/jpeg"}}},
		},
	}

	if msgs := session.buildSDKMessages(); len(msgs) != 2 {
		t.Fatalf("expected no hoisted message without a source path, got %d messages", len(msgs))
	}
}
