package cmd

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
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
		t.Errorf("second part should be an image (type %q, err %v)", ip.Type, err)
	}
}
